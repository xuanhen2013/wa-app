package bff

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/byte-v-forge/wa-app/internal/waapp/bulkregistration"
	"github.com/byte-v-forge/wa-app/internal/waapp/engine"
	"github.com/byte-v-forge/wa-app/internal/waapp/rpc"
	"github.com/byte-v-forge/wa-app/internal/waapp/runtime"
	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
	"github.com/byte-v-forge/wa-app/internal/waapp/smsotp"
	"github.com/byte-v-forge/wa-app/internal/waapp/store"
)

func TestBulkWorkerFailsWhenSupplierHasNoNumber(t *testing.T) {
	provider := &bulkTestProvider{acquireErr: errors.New("NO_NUMBERS")}
	manager, task, item := newBulkTestManager(t, provider, bulkregistration.ItemStatusQueued)
	if err := manager.processTask(context.Background(), task); err != nil {
		t.Fatalf("process task: %v", err)
	}
	updated := bulkTestItem(t, manager, item.ItemID)
	if updated.Status != bulkregistration.ItemStatusFailed || updated.LastError == "" {
		t.Fatalf("unexpected no-number item: %#v", updated)
	}
}

func TestBulkWorkerFailsBeforePurchasingWhenDedicatedProxyIsUnavailable(t *testing.T) {
	provider := &bulkTestProvider{}
	manager, task, item := newBulkTestManager(t, provider, bulkregistration.ItemStatusQueued)
	manager.registrationProxy = RegistrationProxyConfig{Enabled: true, Fallback: "reject"}
	if err := manager.processTask(context.Background(), task); err != nil {
		t.Fatalf("process task: %v", err)
	}
	updated := bulkTestItem(t, manager, item.ItemID)
	if updated.Status != bulkregistration.ItemStatusFailed || provider.acquireCalls != 0 || updated.LastError == "" {
		t.Fatalf("unexpected dedicated-proxy preflight result: item=%#v acquire_calls=%d", updated, provider.acquireCalls)
	}
}

func TestBulkWorkerCancelsActivationWhenWARequestFails(t *testing.T) {
	provider := &bulkTestProvider{}
	manager, task, item := newBulkTestManager(t, provider, bulkregistration.ItemStatusNumberAcquired)
	item.ActivationID = "act_1"
	item.PhoneE164 = "+639171234567"
	item.PhoneMasked = bulkregistration.MaskPhone(item.PhoneE164)
	item.CountryCallingCode = "63"
	item.CountryISO2 = "PH"
	if err := manager.server.Store().SaveItem(context.Background(), item); err != nil {
		t.Fatalf("save item: %v", err)
	}
	if err := manager.failItem(context.Background(), task, item, "WA verification request rejected", true); err != nil {
		t.Fatalf("handle WA request failure: %v", err)
	}
	updated := bulkTestItem(t, manager, item.ItemID)
	if updated.Status != bulkregistration.ItemStatusFailed || provider.cancelCalls != 1 {
		t.Fatalf("unexpected WA request failure cleanup: item=%#v cancel_calls=%d", updated, provider.cancelCalls)
	}
}

func TestBulkWorkerCancelsActivationAfterSMSTimeout(t *testing.T) {
	provider := &bulkTestProvider{}
	manager, task, item := newBulkTestManager(t, provider, bulkregistration.ItemStatusWaitingSMS)
	manager.smsWaitTimeout = 0
	item.ActivationID = "act_1"
	item.PhoneE164 = "+639171234567"
	item.CountryCallingCode = "63"
	item.CountryISO2 = "PH"
	item.WAAccountID = "waacc_1"
	item.VerificationRequestID = "wareq_1"
	if err := manager.server.Store().SaveItem(context.Background(), item); err != nil {
		t.Fatalf("save item: %v", err)
	}
	if err := manager.processTask(context.Background(), task); err != nil {
		t.Fatalf("process SMS timeout: %v", err)
	}
	updated := bulkTestItem(t, manager, item.ItemID)
	if updated.Status != bulkregistration.ItemStatusFailed || provider.cancelCalls != 1 {
		t.Fatalf("unexpected SMS timeout cleanup: item=%#v cancel_calls=%d", updated, provider.cancelCalls)
	}
}

func TestBulkWorkerCancelsOpenItemsOnUserCancellation(t *testing.T) {
	provider := &bulkTestProvider{}
	manager, task, item := newBulkTestManager(t, provider, bulkregistration.ItemStatusNumberAcquired)
	task.Status = bulkregistration.TaskStatusCancelRequested
	item.ActivationID = "act_1"
	item.PhoneE164 = "+639171234567"
	item.CountryCallingCode = "63"
	item.CountryISO2 = "PH"
	if err := manager.server.Store().SaveTask(context.Background(), *task); err != nil {
		t.Fatalf("save task: %v", err)
	}
	if err := manager.server.Store().SaveItem(context.Background(), item); err != nil {
		t.Fatalf("save item: %v", err)
	}
	if err := manager.processTask(context.Background(), task); err != nil {
		t.Fatalf("process cancellation: %v", err)
	}
	updatedTask, err := manager.server.Store().GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	updatedItem := bulkTestItem(t, manager, item.ItemID)
	if updatedTask.Status != bulkregistration.TaskStatusCanceled || updatedItem.Status != bulkregistration.ItemStatusNumberCanceled || provider.cancelCalls != 1 {
		t.Fatalf("unexpected user cancellation: task=%#v item=%#v cancel_calls=%d", updatedTask, updatedItem, provider.cancelCalls)
	}
}

func TestBulkWorkerKeepsCancelPendingWhenSupplierCancelFails(t *testing.T) {
	provider := &bulkTestProvider{cancelErr: errors.New("EARLY_CANCEL_DENIED")}
	manager, task, item := newBulkTestManager(t, provider, bulkregistration.ItemStatusNumberAcquired)
	manager.cancelRetryMax = 1
	item.ActivationID = "act_1"
	if err := manager.server.Store().SaveItem(context.Background(), item); err != nil {
		t.Fatalf("save item: %v", err)
	}
	if err := manager.cancelItem(context.Background(), task, item); err != nil {
		t.Fatalf("cancel item: %v", err)
	}
	updated := bulkTestItem(t, manager, item.ItemID)
	if updated.Status != bulkregistration.ItemStatusCancelPending || provider.cancelCalls != 1 {
		t.Fatalf("unexpected cancellation failure state: item=%#v cancel_calls=%d", updated, provider.cancelCalls)
	}
}

func TestBulkWorkerRejectsAndCancelsNumberFromWrongCountry(t *testing.T) {
	provider := &bulkTestProvider{activation: smsotp.Activation{ActivationID: "act_1", PhoneE164: "+14155550123", CountryISO2: "US", Price: 0.15, Currency: "USD"}}
	manager, task, item := newBulkTestManager(t, provider, bulkregistration.ItemStatusQueued)
	if err := manager.processTask(context.Background(), task); err != nil {
		t.Fatalf("process wrong-country activation: %v", err)
	}
	updated := bulkTestItem(t, manager, item.ItemID)
	if updated.Status != bulkregistration.ItemStatusFailed || provider.cancelCalls != 1 || updated.LastError == "" {
		t.Fatalf("unexpected wrong-country cleanup: item=%#v cancel_calls=%d", updated, provider.cancelCalls)
	}
}

func TestBulkWorkerPreservesFailureReasonWhenCancellationIsPending(t *testing.T) {
	provider := &bulkTestProvider{cancelErr: errors.New("EARLY_CANCEL_DENIED")}
	manager, task, item := newBulkTestManager(t, provider, bulkregistration.ItemStatusNumberAcquired)
	manager.cancelRetryMax = 1
	item.ActivationID = "act_1"
	if err := manager.failItem(context.Background(), task, item, "WA verification request rejected", true); err != nil {
		t.Fatalf("fail item: %v", err)
	}
	updated := bulkTestItem(t, manager, item.ItemID)
	if updated.Status != bulkregistration.ItemStatusCancelPending || !containsAll(updated.LastError, "WA verification request rejected", "cancellation is pending") {
		t.Fatalf("unexpected cancellation-pending item: %#v", updated)
	}
}

func TestBulkTaskCreationReturnsTheExistingActiveTask(t *testing.T) {
	provider := &bulkTestProvider{offers: []smsotp.Offer{{OfferID: "fake-offer", Provider: "fake", CountryISO2: "PH", Service: "whatsapp", Price: 0.15, Currency: "USD", AvailableCount: 10}}}
	manager, task, _ := newBulkTestManager(t, provider, bulkregistration.ItemStatusQueued)
	detail, existing, err := manager.CreateTask(context.Background(), bulkTaskCreateInput{CountryISO2: "PH", TargetCount: 1})
	if err != nil {
		t.Fatalf("create duplicate active task: %v", err)
	}
	if !existing || detail.Task == nil || detail.Task.TaskID != task.TaskID {
		t.Fatalf("expected existing active task, got detail=%#v existing=%t", detail, existing)
	}
}

func TestBulkCountriesUseHeroSMSAnd1024ProxyIntersection(t *testing.T) {
	provider := &bulkTestProvider{countries: []smsotp.Country{
		{CountryISO2: "PH", Name: "菲律宾"},
		{CountryISO2: "US", Name: "美国"},
		{CountryISO2: "CN", Name: "中国"},
	}}
	manager, _, _ := newBulkTestManager(t, provider, bulkregistration.ItemStatusQueued)
	countries, err := manager.ListCountries(context.Background())
	if err != nil {
		t.Fatalf("list countries: %v", err)
	}
	byISO2 := map[string]bool{}
	for _, country := range countries {
		byISO2[country.CountryISO2] = true
	}
	if len(countries) != 2 || !byISO2["PH"] || !byISO2["US"] {
		t.Fatalf("unexpected country intersection: %#v", countries)
	}
}

func TestBulkOffersRejectCountriesOutsideTheCurrentIntersection(t *testing.T) {
	provider := &bulkTestProvider{countries: []smsotp.Country{{CountryISO2: "PH", Name: "菲律宾"}}}
	manager, _, _ := newBulkTestManager(t, provider, bulkregistration.ItemStatusQueued)
	_, err := manager.ListOffers(context.Background(), "US", bulkRegistrationService)
	if err == nil || provider.offerCalls != 0 {
		t.Fatalf("unsupported country must fail before the provider offer request: err=%v calls=%d", err, provider.offerCalls)
	}
}

func newBulkTestManager(t *testing.T, provider smsotp.Provider, itemStatus string) (*bulkRegistrationManager, *bulkregistration.Task, bulkregistration.Item) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wa-app.sqlite3")
	persistentStore, err := store.NewSQLiteStoreFile(ctx, path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(persistentStore.Close)
	runtimeState, err := runtime.NewSQLiteRuntimeFile(ctx, path)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtimeState.Close() })
	nativeEngine, err := engine.NewNativeEngine(persistentStore, shared.SystemClock{}, shared.RandomIDGenerator{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	server := rpc.NewServer(persistentStore, runtimeState, nativeEngine, shared.SystemClock{}, shared.RandomIDGenerator{})
	manager := newBulkRegistrationManager(server, RegistrationProxyConfig{}, BulkRegistrationConfig{Enabled: true, HeroSMSKey: "test"})
	manager.provider = provider
	now := time.Now().UTC()
	task := &bulkregistration.Task{TaskID: "wabulk_test", Status: bulkregistration.TaskStatusRunning, CountryISO2: "PH", TargetCount: 1, CreatedAt: now, UpdatedAt: now}
	item := bulkregistration.Item{ItemID: "wabulki_test", TaskID: task.TaskID, Status: itemStatus, Provider: "fake", OfferID: "fake-offer", Price: 0.15, Currency: "USD", CreatedAt: now, UpdatedAt: now}
	created, existing, err := persistentStore.CreateTask(ctx, *task, []bulkregistration.Item{item})
	if err != nil || existing || created == nil {
		t.Fatalf("create test task: task=%#v existing=%t err=%v", created, existing, err)
	}
	return manager, task, item
}

func bulkTestItem(t *testing.T, manager *bulkRegistrationManager, itemID string) bulkregistration.Item {
	t.Helper()
	items, err := manager.server.Store().ListItems(context.Background(), "wabulk_test")
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	for _, item := range items {
		if item.ItemID == itemID {
			return item
		}
	}
	t.Fatalf("item %s not found", itemID)
	return bulkregistration.Item{}
}

type bulkTestProvider struct {
	acquireErr   error
	cancelErr    error
	activation   smsotp.Activation
	offers       []smsotp.Offer
	countries    []smsotp.Country
	offerCalls   int
	acquireCalls int
	cancelCalls  int
}

func (p *bulkTestProvider) Name() string { return "fake" }

func (p *bulkTestProvider) ListOffers(context.Context, string, string) ([]smsotp.Offer, error) {
	p.offerCalls++
	return p.offers, nil
}

func (p *bulkTestProvider) ListCountries(context.Context) ([]smsotp.Country, error) {
	if p.countries != nil {
		return p.countries, nil
	}
	return []smsotp.Country{{CountryISO2: "PH", Name: "菲律宾"}}, nil
}

func (p *bulkTestProvider) AcquireNumber(context.Context, smsotp.AcquireInput) (smsotp.Activation, error) {
	p.acquireCalls++
	if p.acquireErr != nil {
		return smsotp.Activation{}, p.acquireErr
	}
	if p.activation.ActivationID != "" {
		return p.activation, nil
	}
	return smsotp.Activation{ActivationID: "act_1", PhoneE164: "+639171234567", CountryISO2: "PH", Price: 0.15, Currency: "USD"}, nil
}

func (p *bulkTestProvider) MarkReady(context.Context, string) error { return nil }

func (p *bulkTestProvider) PollCode(context.Context, string) (smsotp.ActivationStatus, error) {
	return smsotp.ActivationStatus{Status: "STATUS_WAIT_CODE"}, nil
}

func (p *bulkTestProvider) Complete(context.Context, string) error { return nil }

func (p *bulkTestProvider) Cancel(context.Context, string) error {
	p.cancelCalls++
	return p.cancelErr
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
