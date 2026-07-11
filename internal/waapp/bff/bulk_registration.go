package bff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/byte-v-forge/wa-app/internal/waapp/bulkregistration"
	"github.com/byte-v-forge/wa-app/internal/waapp/countrycatalog"
	"github.com/byte-v-forge/wa-app/internal/waapp/rpc"
	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
	"github.com/byte-v-forge/wa-app/internal/waapp/smsotp"
	"github.com/byte-v-forge/wa-app/internal/waapp/wacore"
	"github.com/nyaruka/phonenumbers"
)

const (
	bulkRegistrationService          = "whatsapp"
	bulkRegistrationMaxItems         = 100
	bulkRegistrationEventLimit       = 100
	bulkRegistrationPollInterval     = 5 * time.Second
	bulkRegistrationSMSWaitTimeout   = 20 * time.Minute
	bulkRegistrationCancelRetryMax   = 5
	bulkRegistrationIdlePollInterval = 2 * time.Second
	bulkCancellationPendingPrefix    = "SMS activation cancellation pending"
)

type BulkRegistrationConfig struct {
	Enabled     bool
	MaxItems    int
	Concurrency int
	HeroSMSKey  string
	SMSBowerKey string
}

func normalizeBulkRegistrationConfig(config BulkRegistrationConfig) BulkRegistrationConfig {
	if config.MaxItems <= 0 {
		config.MaxItems = bulkRegistrationMaxItems
	}
	if config.MaxItems > bulkRegistrationMaxItems {
		config.MaxItems = bulkRegistrationMaxItems
	}
	if config.Concurrency <= 0 {
		config.Concurrency = config.MaxItems
	}
	if config.Concurrency > config.MaxItems {
		config.Concurrency = config.MaxItems
	}
	return config
}

type bulkRegistrationManager struct {
	server            *rpc.Server
	registrationProxy RegistrationProxyConfig
	config            BulkRegistrationConfig
	providers         map[string]smsotp.Provider
	wake              chan struct{}
	taskMu            sync.Mutex
	proxyPoolMu       sync.Mutex
	proxyPools        map[string]*bulkRegistrationProxyPool
	proxyResolver     func(RegistrationProxyConfig) *registrationProxyResolver
	pollInterval      time.Duration
	smsWaitTimeout    time.Duration
	cancelRetryMax    int
}

func newBulkRegistrationManager(server *rpc.Server, registrationProxy RegistrationProxyConfig, config BulkRegistrationConfig) *bulkRegistrationManager {
	config = normalizeBulkRegistrationConfig(config)
	return &bulkRegistrationManager{
		server:            server,
		registrationProxy: registrationProxy,
		config:            config,
		providers:         newBulkRegistrationProviders(config),
		wake:              make(chan struct{}, 1),
		proxyPools:        map[string]*bulkRegistrationProxyPool{},
		proxyResolver:     newRegistrationProxyResolver,
		pollInterval:      bulkRegistrationPollInterval,
		smsWaitTimeout:    bulkRegistrationSMSWaitTimeout,
		cancelRetryMax:    bulkRegistrationCancelRetryMax,
	}
}

func newBulkRegistrationProviders(config BulkRegistrationConfig) map[string]smsotp.Provider {
	providers := map[string]smsotp.Provider{}
	if strings.TrimSpace(config.HeroSMSKey) != "" {
		provider := smsotp.NewHeroSMSProvider(config.HeroSMSKey)
		providers[provider.Name()] = provider
	}
	if strings.TrimSpace(config.SMSBowerKey) != "" {
		provider := smsotp.NewSMSBowerProvider(config.SMSBowerKey)
		providers[provider.Name()] = provider
	}
	return providers
}

func normalizeBulkProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hero", "herosms", "hero-sms":
		return "hero-sms"
	case "sms-bower", "sms_bower", "smsbower":
		return "smsbower"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func (m *bulkRegistrationManager) provider(name string) (smsotp.Provider, error) {
	if m == nil {
		return nil, fmt.Errorf("bulk registration is unavailable")
	}
	name = normalizeBulkProvider(name)
	if name == "" {
		return nil, fmt.Errorf("SMS provider is required")
	}
	provider := m.providers[name]
	if provider == nil {
		return nil, fmt.Errorf("SMS provider is not configured")
	}
	return provider, nil
}

func (m *bulkRegistrationManager) providerForItem(task bulkregistration.Task, item bulkregistration.Item) (smsotp.Provider, error) {
	taskProvider := normalizeBulkProvider(task.Provider)
	itemProvider := normalizeBulkProvider(item.Provider)
	if taskProvider != "" && itemProvider != "" && taskProvider != itemProvider {
		return nil, fmt.Errorf("task and SMS activation providers do not match")
	}
	providerName := taskProvider
	if providerName == "" {
		providerName = itemProvider
	}
	if providerName == "" && len(m.providers) == 1 {
		for name := range m.providers {
			providerName = name
		}
	}
	return m.provider(providerName)
}

func (m *bulkRegistrationManager) Run(ctx context.Context) error {
	if m == nil || m.server == nil {
		<-ctx.Done()
		return nil
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-m.wake:
		case <-timer.C:
		}
		if err := m.runNext(ctx); err != nil && ctx.Err() == nil {
			log.Printf("wa_bulk_registration_worker error=%s", shared.ProbeLogValue(err.Error()))
		}
		timer.Reset(bulkRegistrationIdlePollInterval)
	}
}

func (m *bulkRegistrationManager) runNext(ctx context.Context) error {
	if !m.enabled() {
		return nil
	}
	task, err := m.server.Store().GetActiveTask(ctx)
	if err != nil || task == nil {
		return err
	}
	if err := m.processTask(ctx, task); err != nil {
		if ctx.Err() == nil {
			m.taskMu.Lock()
			current, loadErr := m.server.Store().GetTask(context.Background(), task.TaskID)
			if loadErr == nil && current != nil && current.Status == bulkregistration.TaskStatusRunning {
				current.LastError = compactBulkError(err)
				current.UpdatedAt = time.Now().UTC()
				_ = m.server.Store().SaveTask(context.Background(), *current)
			}
			m.taskMu.Unlock()
		}
		return err
	}
	return nil
}

func (m *bulkRegistrationManager) ListProviders() []string {
	if m == nil {
		return []string{}
	}
	result := make([]string, 0, len(m.providers))
	for name := range m.providers {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (m *bulkRegistrationManager) ListOffers(ctx context.Context, providerName string, countryISO2 string, service string) ([]bulkregistration.Offer, error) {
	if !m.enabled() {
		return nil, fmt.Errorf("bulk registration is disabled")
	}
	provider, err := m.provider(providerName)
	if err != nil {
		return nil, err
	}
	countryISO2 = normalizeBulkCountry(countryISO2)
	if err := m.requireSupportedCountry(ctx, provider.Name(), countryISO2); err != nil {
		return nil, err
	}
	offers, err := provider.ListOffers(ctx, countryISO2, service)
	if err != nil {
		return nil, err
	}
	result := make([]bulkregistration.Offer, 0, len(offers))
	for _, offer := range offers {
		result = append(result, bulkOfferFromProvider(offer))
	}
	sort.SliceStable(result, func(left int, right int) bool {
		return result[left].Price < result[right].Price
	})
	return result, nil
}

// ListCountries returns only countries that the selected SMS provider exposes
// and 1024proxy supports as a country-level registration exit.
func (m *bulkRegistrationManager) ListCountries(ctx context.Context, providerName string) ([]smsotp.Country, error) {
	if !m.enabled() {
		return nil, fmt.Errorf("bulk registration is disabled")
	}
	provider, err := m.provider(providerName)
	if err != nil {
		return nil, err
	}
	lister, ok := provider.(smsotp.CountryLister)
	if !ok {
		return nil, fmt.Errorf("SMS provider does not expose countries")
	}
	providerCountries, err := lister.ListCountries(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]smsotp.Country, 0, len(providerCountries))
	seen := map[string]struct{}{}
	for _, country := range providerCountries {
		countryISO2 := normalizeBulkCountry(country.CountryISO2)
		if !countrycatalog.SupportsRegistrationProxy1024Country(countryISO2) {
			continue
		}
		if _, ok := seen[countryISO2]; ok {
			continue
		}
		seen[countryISO2] = struct{}{}
		name := strings.TrimSpace(country.Name)
		if name == "" {
			name = countryISO2
		}
		result = append(result, smsotp.Country{CountryISO2: countryISO2, Name: name})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Name == result[right].Name {
			return result[left].CountryISO2 < result[right].CountryISO2
		}
		return result[left].Name < result[right].Name
	})
	return result, nil
}

func (m *bulkRegistrationManager) requireSupportedCountry(ctx context.Context, providerName string, countryISO2 string) error {
	countryISO2 = normalizeBulkCountry(countryISO2)
	if countryISO2 == "" || !countrycatalog.SupportsRegistrationProxy1024Country(countryISO2) {
		return fmt.Errorf("country is not supported by the registration proxy")
	}
	countries, err := m.ListCountries(ctx, providerName)
	if err != nil {
		return err
	}
	for _, country := range countries {
		if country.CountryISO2 == countryISO2 {
			return nil
		}
	}
	return fmt.Errorf("country is not currently available from the selected SMS provider")
}

func (m *bulkRegistrationManager) CreateTask(ctx context.Context, input bulkTaskCreateInput) (*bulkregistration.TaskDetail, bool, error) {
	if !m.enabled() {
		return nil, false, fmt.Errorf("bulk registration is disabled")
	}
	providerName := normalizeBulkProvider(input.Provider)
	if _, err := m.provider(providerName); err != nil {
		return nil, false, err
	}
	countryISO2 := normalizeBulkCountry(input.CountryISO2)
	if countryISO2 == "" {
		return nil, false, fmt.Errorf("country_iso2 is required")
	}
	if input.TargetCount <= 0 || input.TargetCount > m.config.MaxItems {
		return nil, false, fmt.Errorf("target_count must be between 1 and %d", m.config.MaxItems)
	}
	concurrency, err := normalizeBulkTaskConcurrency(input.Concurrency, input.TargetCount, m.config.Concurrency)
	if err != nil {
		return nil, false, err
	}
	offers, err := m.ListOffers(ctx, providerName, countryISO2, bulkRegistrationService)
	if err != nil {
		return nil, false, err
	}
	selections, err := normalizeBulkSelections(input.Selections, input.TargetCount, providerName, offers)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	task := bulkregistration.Task{
		TaskID:        m.server.IDs().NewID("wabulk_"),
		Status:        bulkregistration.TaskStatusRunning,
		Provider:      providerName,
		CountryISO2:   countryISO2,
		TargetCount:   input.TargetCount,
		Concurrency:   concurrency,
		IntegrityMode: strings.TrimSpace(input.IntegrityMode),
		Selections:    selections,
		CreatedAt:     now,
		UpdatedAt:     now,
		StartedAt:     &now,
	}
	items := bulkItemsForTask(m.server, task, offers, now)
	created, existing, err := m.server.Store().CreateTask(ctx, task, items)
	if err != nil {
		return nil, false, err
	}
	detail, err := m.TaskDetail(ctx, created.TaskID)
	if err != nil {
		return nil, false, err
	}
	if !existing {
		m.signal()
	}
	return detail, existing, nil
}

func (m *bulkRegistrationManager) TaskDetail(ctx context.Context, taskID string) (*bulkregistration.TaskDetail, error) {
	var (
		task *bulkregistration.Task
		err  error
	)
	if strings.TrimSpace(taskID) == "" {
		task, err = m.server.Store().GetActiveTask(ctx)
	} else {
		task, err = m.server.Store().GetTask(ctx, taskID)
	}
	if err != nil {
		return nil, err
	}
	if task == nil {
		if taskID != "" {
			return nil, bulkregistration.ErrTaskNotFound
		}
		return &bulkregistration.TaskDetail{Items: []bulkregistration.Item{}, Events: []bulkregistration.Event{}}, nil
	}
	items, err := m.server.Store().ListItems(ctx, task.TaskID)
	if err != nil {
		return nil, err
	}
	events, err := m.server.Store().ListEvents(ctx, task.TaskID, bulkRegistrationEventLimit)
	if err != nil {
		return nil, err
	}
	return &bulkregistration.TaskDetail{Task: task, Items: items, Events: events}, nil
}

func (m *bulkRegistrationManager) LatestTaskDetail(ctx context.Context) (*bulkregistration.TaskDetail, error) {
	task, err := m.server.Store().GetLatestTask(ctx)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return &bulkregistration.TaskDetail{Items: []bulkregistration.Item{}, Events: []bulkregistration.Event{}}, nil
	}
	return m.TaskDetail(ctx, task.TaskID)
}

func (m *bulkRegistrationManager) CancelTask(ctx context.Context) (*bulkregistration.TaskDetail, error) {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()
	task, err := m.server.Store().GetActiveTask(ctx)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, bulkregistration.ErrTaskNotFound
	}
	if task.Status != bulkregistration.TaskStatusCancelRequested && task.Status != bulkregistration.TaskStatusCanceling {
		now := time.Now().UTC()
		task.Status = bulkregistration.TaskStatusCancelRequested
		task.CancelRequestedAt = &now
		task.UpdatedAt = now
		if err := m.server.Store().SaveTask(ctx, *task); err != nil {
			return nil, err
		}
	}
	m.signal()
	return m.TaskDetail(ctx, task.TaskID)
}

func (m *bulkRegistrationManager) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/wa/bulk-registration/"), "/")
	switch path {
	case "providers":
		m.handleProviders(w, r)
	case "countries":
		m.handleCountries(w, r)
	case "offers":
		m.handleOffers(w, r)
	case "task":
		m.handleTask(w, r)
	case "task/cancel":
		m.handleCancelTask(w, r)
	default:
		bulkJSON(w, http.StatusNotFound, map[string]string{"error": "unknown bulk registration endpoint"})
	}
}

func (m *bulkRegistrationManager) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bulkMethodNotAllowed(w, http.MethodGet)
		return
	}
	bulkJSON(w, http.StatusOK, map[string]any{"success": true, "providers": m.ListProviders()})
}

func (m *bulkRegistrationManager) handleCountries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bulkMethodNotAllowed(w, http.MethodGet)
		return
	}
	providerName := normalizeBulkProvider(r.URL.Query().Get("provider"))
	countries, err := m.ListCountries(r.Context(), providerName)
	if err != nil {
		bulkError(w, err)
		return
	}
	bulkJSON(w, http.StatusOK, map[string]any{"success": true, "provider": providerName, "countries": countries})
}

func (m *bulkRegistrationManager) handleOffers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bulkMethodNotAllowed(w, http.MethodGet)
		return
	}
	countryISO2 := normalizeBulkCountry(r.URL.Query().Get("country_iso2"))
	providerName := normalizeBulkProvider(r.URL.Query().Get("provider"))
	service := shared.FirstNonEmpty(r.URL.Query().Get("service"), bulkRegistrationService)
	offers, err := m.ListOffers(r.Context(), providerName, countryISO2, service)
	if err != nil {
		bulkError(w, err)
		return
	}
	bulkJSON(w, http.StatusOK, map[string]any{"success": true, "provider": providerName, "country_iso2": countryISO2, "service": bulkRegistrationService, "offers": offers, "max_items": m.config.MaxItems, "max_concurrency": m.config.Concurrency})
}

func (m *bulkRegistrationManager) handleTask(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		detail, err := m.TaskDetail(r.Context(), "")
		if err != nil {
			bulkError(w, err)
			return
		}
		latest := &bulkregistration.TaskDetail{Items: []bulkregistration.Item{}, Events: []bulkregistration.Event{}}
		if detail.Task == nil {
			latest, err = m.LatestTaskDetail(r.Context())
			if err != nil {
				bulkError(w, err)
				return
			}
		}
		bulkJSON(w, http.StatusOK, map[string]any{"success": true, "task": detail.Task, "items": detail.Items, "events": detail.Events, "last_task": latest.Task, "last_items": latest.Items, "last_events": latest.Events, "max_items": m.config.MaxItems, "max_concurrency": m.config.Concurrency})
	case http.MethodPost:
		input := bulkTaskCreateInput{}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
			bulkJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be valid JSON"})
			return
		}
		detail, existing, err := m.CreateTask(r.Context(), input)
		if err != nil {
			bulkError(w, err)
			return
		}
		bulkJSON(w, http.StatusOK, map[string]any{"success": true, "existing": existing, "task": detail.Task, "items": detail.Items, "events": detail.Events, "max_items": m.config.MaxItems, "max_concurrency": m.config.Concurrency})
	default:
		bulkMethodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (m *bulkRegistrationManager) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bulkMethodNotAllowed(w, http.MethodPost)
		return
	}
	detail, err := m.CancelTask(r.Context())
	if err != nil {
		bulkError(w, err)
		return
	}
	bulkJSON(w, http.StatusOK, map[string]any{"success": true, "task": detail.Task, "items": detail.Items, "events": detail.Events})
}

func (m *bulkRegistrationManager) processTask(ctx context.Context, task *bulkregistration.Task) error {
	if task.Status == bulkregistration.TaskStatusPaused {
		return nil
	}
	items, err := m.server.Store().ListItems(ctx, task.TaskID)
	if err != nil {
		return err
	}
	if task.Status == bulkregistration.TaskStatusCancelRequested || task.Status == bulkregistration.TaskStatusCanceling {
		return m.cancelTaskItems(ctx, task, items)
	}
	concurrency := taskConcurrency(*task, m.config.Concurrency)
	workers := make(chan struct{}, concurrency)
	errs := make(chan error, len(items))
	var waitGroup sync.WaitGroup
	for _, item := range items {
		if bulkregistration.IsTerminalItemStatus(item.Status) {
			continue
		}
		item := item
		workerTask := *task
		workers <- struct{}{}
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			defer func() { <-workers }()
			if err := m.processItem(ctx, &workerTask, item); err != nil {
				errs <- err
			}
		}()
	}
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return m.finishTaskIfDone(ctx, task)
}

func (m *bulkRegistrationManager) processItem(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item) error {
	if taskCancelRequested(ctx, m.server, task.TaskID) {
		return m.cancelItem(ctx, task, item)
	}
	switch item.Status {
	case bulkregistration.ItemStatusQueued:
		return m.acquireAndStart(ctx, task, item)
	case bulkregistration.ItemStatusAcquiringNumber:
		return m.failItem(ctx, task, item, "service restarted while acquiring a number", true)
	case bulkregistration.ItemStatusNumberAcquired, bulkregistration.ItemStatusWAProbing, bulkregistration.ItemStatusRequestingOTP:
		return m.startWARegistration(ctx, task, item, nil)
	case bulkregistration.ItemStatusWaitingSMS, bulkregistration.ItemStatusSMSReceived, bulkregistration.ItemStatusSubmittingOTP:
		return m.waitForSMSAndSubmit(ctx, task, item)
	case bulkregistration.ItemStatusCancelPending, bulkregistration.ItemStatusCancelingNumber:
		return m.cancelItem(ctx, task, item)
	default:
		return m.failItem(ctx, task, item, "unknown item state", true)
	}
}

func (m *bulkRegistrationManager) acquireAndStart(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item) error {
	route, err := m.preflightRegistrationProxy(ctx, task, item)
	if err != nil {
		return m.failItem(ctx, task, item, compactBulkError(err), false)
	}
	item.Status = bulkregistration.ItemStatusAcquiringNumber
	item.AttemptCount++
	if err := m.saveItem(ctx, task, &item, "acquiring_number", "", ""); err != nil {
		return err
	}
	provider, err := m.providerForItem(*task, item)
	if err != nil {
		return m.failItem(ctx, task, item, compactBulkError(err), false)
	}
	activation, err := provider.AcquireNumber(ctx, smsotp.AcquireInput{CountryISO2: task.CountryISO2, Offer: smsOfferFromItem(item)})
	if err != nil {
		return m.failItem(ctx, task, item, compactBulkError(err), false)
	}
	if activation.Price > item.Price+0.000001 {
		item.ActivationID = activation.ActivationID
		return m.failItem(ctx, task, item, "SMS provider returned an activation above the selected price", true)
	}
	phone, err := bulkPhoneTarget(activation.PhoneE164, task.CountryISO2)
	if err != nil {
		item.ActivationID = activation.ActivationID
		return m.failItem(ctx, task, item, compactBulkError(err), true)
	}
	item.Status = bulkregistration.ItemStatusNumberAcquired
	item.ActivationID = activation.ActivationID
	item.PhoneE164 = phone.E164Number
	item.PhoneMasked = bulkregistration.MaskPhone(phone.E164Number)
	item.CountryCallingCode = phone.CountryCallingCode
	item.CountryISO2 = phone.CountryIso2
	if operator := strings.TrimSpace(activation.Operator); operator != "" {
		item.Operator = operator
	}
	item.Price = activation.Price
	item.Currency = activation.Currency
	item.SMSStatus = "NUMBER_ACQUIRED"
	if err := m.saveItem(ctx, task, &item, "number_acquired", item.SMSStatus, ""); err != nil {
		return err
	}
	if err := provider.MarkReady(ctx, item.ActivationID); err != nil {
		return m.failItem(ctx, task, item, "could not prepare the SMS activation", true)
	}
	return m.startWARegistration(ctx, task, item, route)
}

// preflightRegistrationProxy prevents an unavailable dedicated route from
// consuming a paid SMS activation before the WA registration can begin.
func (m *bulkRegistrationManager) preflightRegistrationProxy(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item) (*wacore.WAProxyRoute, error) {
	resolver := m.newRegistrationProxyResolver()
	if !resolver.enabled() {
		return nil, nil
	}
	pool := m.registrationProxyPool(*task, resolver)
	return pool.lease(ctx, item.ItemID)
}

func (m *bulkRegistrationManager) newRegistrationProxyResolver() *registrationProxyResolver {
	if m != nil && m.proxyResolver != nil {
		return m.proxyResolver(m.registrationProxy)
	}
	return newRegistrationProxyResolver(m.registrationProxy)
}

func (m *bulkRegistrationManager) registrationProxyPool(task bulkregistration.Task, resolver *registrationProxyResolver) *bulkRegistrationProxyPool {
	key := task.TaskID + "\x00" + normalizeBulkCountry(task.CountryISO2)
	m.proxyPoolMu.Lock()
	defer m.proxyPoolMu.Unlock()
	if pool := m.proxyPools[key]; pool != nil {
		return pool
	}
	pool := newBulkRegistrationProxyPool(task, resolver)
	m.proxyPools[key] = pool
	return pool
}

func (m *bulkRegistrationManager) discardRegistrationProxyPool(taskID string) {
	if m == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	m.proxyPoolMu.Lock()
	for key := range m.proxyPools {
		if strings.HasPrefix(key, taskID+"\x00") {
			delete(m.proxyPools, key)
		}
	}
	m.proxyPoolMu.Unlock()
}

func (m *bulkRegistrationManager) startWARegistration(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item, route *wacore.WAProxyRoute) error {
	if item.PhoneE164 == "" || item.ActivationID == "" {
		return m.failItem(ctx, task, item, "item is missing a phone activation", true)
	}
	if route == nil {
		var err error
		route, err = m.preflightRegistrationProxy(ctx, task, item)
		if err != nil {
			return m.failItem(ctx, task, item, compactBulkError(err), true)
		}
	}
	item.Status = bulkregistration.ItemStatusWAProbing
	item.WAProbeStatus = "RUNNING"
	if err := m.saveItem(ctx, task, &item, "wa_registration_started", item.SMSStatus, item.WAProbeStatus); err != nil {
		return err
	}
	payload := map[string]any{
		"e164_number":          item.PhoneE164,
		"country_calling_code": item.CountryCallingCode,
		"country_iso2":         item.CountryISO2,
		"delivery_method":      "sms",
		"integrity_mode":       task.IntegrityMode,
		"job_id":               task.TaskID + ":" + item.ItemID,
		"correlation_id":       task.TaskID,
	}
	gateway := &actionGateway{server: m.server, registrationProxy: m.newRegistrationProxyResolver(), registrationRoute: route}
	result, err := gateway.startRegistration(ctx, payload)
	if err != nil {
		return m.failItem(ctx, task, item, compactBulkError(err), true)
	}
	item.WAProbeStatus = shared.TextField(shared.ObjectField(result, "phone_status"), "account_status")
	item.WAVerificationStatus = shared.TextField(result, "status")
	item.WAAccountID = shared.TextField(result, "wa_account_id")
	item.VerificationRequestID = shared.TextField(result, "verification_request_id")
	if result["success"] != true || item.WAAccountID == "" || item.VerificationRequestID == "" {
		message := shared.FirstNonEmpty(shared.TextField(result, "error_message"), shared.TextField(result, "reject_reason"), "WA rejected the verification request")
		return m.failItem(ctx, task, item, message, true)
	}
	item.Status = bulkregistration.ItemStatusWaitingSMS
	item.SMSStatus = "STATUS_WAIT_CODE"
	if err := m.saveItem(ctx, task, &item, "wa_otp_requested", item.SMSStatus, item.WAVerificationStatus); err != nil {
		return err
	}
	return nil
}

func (m *bulkRegistrationManager) waitForSMSAndSubmit(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item) error {
	provider, err := m.providerForItem(*task, item)
	if err != nil {
		return m.failItem(ctx, task, item, compactBulkError(err), true)
	}
	deadline := time.Now().UTC().Add(m.smsWaitTimeout)
	for time.Now().UTC().Before(deadline) {
		if taskCancelRequested(ctx, m.server, task.TaskID) {
			return m.cancelItem(ctx, task, item)
		}
		status, err := provider.PollCode(ctx, item.ActivationID)
		if err != nil {
			return m.failItem(ctx, task, item, "could not read the SMS activation", true)
		}
		item.SMSStatus = status.Status
		if status.Code == "" {
			if status.Status == "STATUS_CANCEL" {
				return m.failItem(ctx, task, item, "SMS activation was canceled by the provider", false)
			}
			if err := m.saveItem(ctx, task, &item, "sms_status", status.Status, item.WAVerificationStatus); err != nil {
				return err
			}
			if !waitBulkRegistration(ctx, m.pollInterval) {
				return ctx.Err()
			}
			continue
		}
		item.Status = bulkregistration.ItemStatusSMSReceived
		if err := m.saveItem(ctx, task, &item, "sms_received", status.Status, item.WAVerificationStatus); err != nil {
			return err
		}
		return m.submitOTP(ctx, task, item, status.Code)
	}
	return m.failItem(ctx, task, item, "timed out waiting for the SMS code", true)
}

func (m *bulkRegistrationManager) submitOTP(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item, code string) error {
	item.Status = bulkregistration.ItemStatusSubmittingOTP
	if err := m.saveItem(ctx, task, &item, "submitting_otp", item.SMSStatus, item.WAVerificationStatus); err != nil {
		return err
	}
	gateway := &actionGateway{server: m.server, registrationProxy: newRegistrationProxyResolver(m.registrationProxy)}
	result, err := gateway.submitOTP(ctx, map[string]any{"verification_request_id": item.VerificationRequestID, "code": code, "job_id": task.TaskID + ":" + item.ItemID, "correlation_id": task.TaskID})
	if err != nil {
		return m.failItem(ctx, task, item, compactBulkError(err), true)
	}
	item.WARegistrationStatus = shared.TextField(result, "status")
	if result["success"] != true {
		message := shared.FirstNonEmpty(shared.TextField(result, "error_message"), shared.TextField(shared.ObjectField(result, "error"), "message"), "WA rejected the OTP")
		return m.failItem(ctx, task, item, message, true)
	}
	item.Status = bulkregistration.ItemStatusRegistered
	item.FinishedAt = timePointer(time.Now().UTC())
	if err := m.saveItem(ctx, task, &item, "registered", item.SMSStatus, item.WARegistrationStatus); err != nil {
		return err
	}
	provider, providerErr := m.providerForItem(*task, item)
	completeErr := providerErr
	if completeErr == nil {
		completeErr = provider.Complete(ctx, item.ActivationID)
	}
	if completeErr != nil {
		item.LastError = "registered, but the SMS activation could not be finalized"
		item.UpdatedAt = time.Now().UTC()
		_ = m.server.Store().SaveItem(context.Background(), item)
		_ = m.appendEvent(context.Background(), task, item, "activation_finish_failed", item.SMSStatus, item.WARegistrationStatus)
	}
	return m.finishTaskIfDone(ctx, task)
}

func (m *bulkRegistrationManager) failItem(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item, message string, cancelActivation bool) error {
	message = compactBulkError(errors.New(message))
	item.LastError = message
	if cancelActivation && item.ActivationID != "" {
		return m.cancelItem(ctx, task, item)
	}
	m.cleanupPendingAccount(item)
	item.Status = bulkregistration.ItemStatusFailed
	item.FinishedAt = timePointer(time.Now().UTC())
	return m.saveItem(ctx, task, &item, "failed", item.SMSStatus, item.WARegistrationStatus)
}

func (m *bulkRegistrationManager) cancelTaskItems(ctx context.Context, task *bulkregistration.Task, items []bulkregistration.Item) error {
	task.Status = bulkregistration.TaskStatusCanceling
	task.UpdatedAt = time.Now().UTC()
	m.taskMu.Lock()
	if err := m.server.Store().SaveTask(ctx, *task); err != nil {
		m.taskMu.Unlock()
		return err
	}
	m.taskMu.Unlock()
	for _, item := range items {
		if item.Status == bulkregistration.ItemStatusRegistered || bulkregistration.IsTerminalItemStatus(item.Status) {
			continue
		}
		if err := m.cancelItem(ctx, task, item); err != nil {
			return err
		}
	}
	updatedItems, err := m.server.Store().ListItems(ctx, task.TaskID)
	if err != nil {
		return err
	}
	for _, item := range updatedItems {
		if item.Status != bulkregistration.ItemStatusRegistered && !bulkregistration.IsTerminalItemStatus(item.Status) {
			return nil
		}
	}
	return m.finishTask(ctx, task)
}

func (m *bulkRegistrationManager) cancelItem(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item) error {
	if item.Status == bulkregistration.ItemStatusRegistered || bulkregistration.IsTerminalItemStatus(item.Status) {
		return nil
	}
	if item.ActivationID == "" {
		item.Status = bulkregistration.ItemStatusCanceled
		item.FinishedAt = timePointer(time.Now().UTC())
		return m.saveItem(ctx, task, &item, "canceled", item.SMSStatus, item.WARegistrationStatus)
	}
	// Cancellation state belongs in Status. Keep LastError focused on the
	// registration failure and replace any older review detail.
	item.LastError = bulkFailureReason(item.LastError)
	item.Status = bulkregistration.ItemStatusCancelingNumber
	if err := m.saveItem(ctx, task, &item, "canceling_activation", item.SMSStatus, item.WARegistrationStatus); err != nil {
		return err
	}
	provider, err := m.providerForItem(*task, item)
	if err != nil {
		item.Status = bulkregistration.ItemStatusCancelPending
		item.LastError = bulkCancellationPendingError(item.LastError, err)
		return m.saveItem(ctx, task, &item, "activation_cancel_pending", item.SMSStatus, item.WARegistrationStatus)
	}
	var cancelErr error
	for attempt := 1; attempt <= m.cancelRetryMax; attempt++ {
		item.CancelAttemptCount = attempt
		cancelErr = provider.Cancel(ctx, item.ActivationID)
		if cancelErr == nil {
			if item.WAAccountID != "" {
				gateway := &actionGateway{server: m.server, registrationProxy: newRegistrationProxyResolver(m.registrationProxy)}
				_, _ = gateway.cleanupFailedRegistration(context.Background(), map[string]any{"wa_account_id": item.WAAccountID, "verification_request_id": item.VerificationRequestID})
			}
			if item.LastError != "" && task.Status != bulkregistration.TaskStatusCancelRequested && task.Status != bulkregistration.TaskStatusCanceling {
				item.Status = bulkregistration.ItemStatusFailed
			} else {
				item.Status = bulkregistration.ItemStatusNumberCanceled
			}
			item.SMSStatus = "STATUS_CANCEL"
			item.FinishedAt = timePointer(time.Now().UTC())
			return m.saveItem(ctx, task, &item, "activation_canceled", item.SMSStatus, item.WARegistrationStatus)
		}
		if attempt < m.cancelRetryMax && !waitBulkRegistration(ctx, time.Duration(attempt)*time.Second) {
			return ctx.Err()
		}
	}
	item.Status = bulkregistration.ItemStatusCancelPending
	item.LastError = bulkCancellationPendingError(item.LastError, cancelErr)
	return m.saveItem(ctx, task, &item, "activation_cancel_pending", item.SMSStatus, item.WARegistrationStatus)
}

func (m *bulkRegistrationManager) finishTaskIfDone(ctx context.Context, task *bulkregistration.Task) error {
	items, err := m.server.Store().ListItems(ctx, task.TaskID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if !bulkregistration.IsTerminalItemStatus(item.Status) {
			return nil
		}
	}
	current, err := m.server.Store().GetTask(ctx, task.TaskID)
	if err != nil {
		return err
	}
	if current == nil {
		return bulkregistration.ErrTaskNotFound
	}
	return m.finishTask(ctx, current)
}

func (m *bulkRegistrationManager) finishTask(ctx context.Context, task *bulkregistration.Task) error {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()
	current, err := m.server.Store().GetTask(ctx, task.TaskID)
	if err != nil {
		return err
	}
	if current == nil {
		return bulkregistration.ErrTaskNotFound
	}
	task = current
	items, err := m.server.Store().ListItems(ctx, task.TaskID)
	if err != nil {
		return err
	}
	success, failed, canceled, waiting := bulkTaskCounts(items)
	task.SuccessCount = success
	task.FailedCount = failed
	task.CanceledCount = canceled
	task.WaitingCount = waiting
	now := time.Now().UTC()
	task.UpdatedAt = now
	if task.Status == bulkregistration.TaskStatusCanceling || task.Status == bulkregistration.TaskStatusCancelRequested {
		task.Status = bulkregistration.TaskStatusCanceled
	} else if success == task.TargetCount {
		task.Status = bulkregistration.TaskStatusCompleted
	} else if success > 0 {
		task.Status = bulkregistration.TaskStatusPartialCompleted
	} else {
		task.Status = bulkregistration.TaskStatusFailed
	}
	task.FinishedAt = &now
	if err := m.server.Store().SaveTask(ctx, *task); err != nil {
		return err
	}
	m.discardRegistrationProxyPool(task.TaskID)
	return nil
}

func (m *bulkRegistrationManager) saveItem(ctx context.Context, task *bulkregistration.Task, item *bulkregistration.Item, eventType string, providerStatus string, waStatus string) error {
	item.UpdatedAt = time.Now().UTC()
	if err := m.server.Store().SaveItem(ctx, *item); err != nil {
		return err
	}
	if err := m.appendEvent(ctx, task, *item, eventType, providerStatus, waStatus); err != nil {
		return err
	}
	return m.refreshTaskProgress(ctx, task)
}

func (m *bulkRegistrationManager) refreshTaskProgress(ctx context.Context, task *bulkregistration.Task) error {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()
	items, err := m.server.Store().ListItems(ctx, task.TaskID)
	if err != nil {
		return err
	}
	current, err := m.server.Store().GetTask(ctx, task.TaskID)
	if err != nil {
		return err
	}
	if current == nil {
		return bulkregistration.ErrTaskNotFound
	}
	success, failed, canceled, waiting := bulkTaskCounts(items)
	current.SuccessCount = success
	current.FailedCount = failed
	current.CanceledCount = canceled
	current.WaitingCount = waiting
	current.UpdatedAt = time.Now().UTC()
	return m.server.Store().SaveTask(ctx, *current)
}

func (m *bulkRegistrationManager) appendEvent(ctx context.Context, task *bulkregistration.Task, item bulkregistration.Item, eventType string, providerStatus string, waStatus string) error {
	message := safeBulkEventMessage(item.LastError)
	event := bulkregistration.Event{EventID: m.server.IDs().NewID("wabulevt_"), TaskID: task.TaskID, ItemID: item.ItemID, Provider: item.Provider, ActivationID: item.ActivationID, EventType: eventType, ProviderStatus: providerStatus, WAStatus: waStatus, Message: message, CreatedAt: time.Now().UTC()}
	if err := m.server.Store().AppendEvent(ctx, event); err != nil {
		return err
	}
	if message != "" {
		log.Printf("wa_bulk_registration_failure task_id=%s item_id=%s stage=%s failure_kind=%s sms_status=%s wa_status=%s", shared.SafeProxyLogToken(task.TaskID, "task"), shared.SafeProxyLogToken(item.ItemID, "item"), shared.SafeProxyLogToken(eventType, "unknown"), bulkFailureKind(message), shared.SafeProxyLogToken(providerStatus, "none"), shared.SafeProxyLogToken(waStatus, "none"))
	}
	return nil
}

func (m *bulkRegistrationManager) enabled() bool {
	return m != nil && m.server != nil && m.config.Enabled && len(m.providers) > 0
}

func (m *bulkRegistrationManager) signal() {
	if m == nil {
		return
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

type bulkTaskCreateInput struct {
	Provider      string                            `json:"provider"`
	CountryISO2   string                            `json:"country_iso2"`
	TargetCount   int                               `json:"target_count"`
	Concurrency   int                               `json:"concurrency"`
	IntegrityMode string                            `json:"integrity_mode"`
	Selections    []bulkregistration.OfferSelection `json:"offers"`
}

func defaultBulkTaskConcurrency(targetCount int) int {
	if targetCount <= 1 {
		return 1
	}
	return targetCount / 3
}

func normalizeBulkTaskConcurrency(value int, targetCount int, maximum int) (int, error) {
	if value == 0 {
		value = defaultBulkTaskConcurrency(targetCount)
	}
	if value < 1 {
		return 0, fmt.Errorf("concurrency must be at least 1")
	}
	if value > targetCount {
		return 0, fmt.Errorf("concurrency must not exceed target_count")
	}
	if value > maximum {
		return 0, fmt.Errorf("concurrency must not exceed the configured maximum of %d", maximum)
	}
	return value, nil
}

func taskConcurrency(task bulkregistration.Task, maximum int) int {
	if task.TargetCount <= 1 {
		return 1
	}
	if task.Concurrency <= 0 {
		return 1
	}
	if task.Concurrency > task.TargetCount {
		return task.TargetCount
	}
	if task.Concurrency > maximum {
		return maximum
	}
	return task.Concurrency
}

func normalizeBulkSelections(input []bulkregistration.OfferSelection, targetCount int, expectedProvider string, offers []bulkregistration.Offer) ([]bulkregistration.OfferSelection, error) {
	byID := map[string]bulkregistration.Offer{}
	for _, offer := range offers {
		byID[offer.OfferID] = offer
	}
	if len(input) == 0 {
		if len(offers) == 0 {
			return nil, fmt.Errorf("no SMS offers are available")
		}
		input = []bulkregistration.OfferSelection{{OfferID: offers[0].OfferID, Quantity: targetCount, MaxPrice: offers[0].Price}}
	}
	result := make([]bulkregistration.OfferSelection, 0, len(input))
	count := 0
	selectedByPriceTier := map[string]int{}
	for _, selection := range input {
		offer, ok := byID[selection.OfferID]
		if !ok {
			return nil, fmt.Errorf("the selected SMS price tier is no longer available")
		}
		if normalizeBulkProvider(offer.Provider) != normalizeBulkProvider(expectedProvider) {
			return nil, fmt.Errorf("the selected SMS offer does not belong to the selected provider")
		}
		if selection.Quantity <= 0 || selection.Quantity > offer.AvailableCount {
			return nil, fmt.Errorf("selected quantity exceeds the available SMS stock")
		}
		priceTier := bulkOfferPriceTierKey(offer)
		selectedByPriceTier[priceTier] += selection.Quantity
		if selectedByPriceTier[priceTier] > offer.AvailableCount {
			return nil, fmt.Errorf("selected quantity exceeds the available SMS stock for this price tier")
		}
		count += selection.Quantity
		selection.MaxPrice = offer.Price
		result = append(result, selection)
	}
	if count != targetCount {
		return nil, fmt.Errorf("selected quantities must equal target_count")
	}
	return result, nil
}

func bulkOfferPriceTierKey(offer bulkregistration.Offer) string {
	priceTier := strings.TrimSpace(offer.PriceTier)
	if priceTier == "" {
		priceTier = strconv.FormatFloat(offer.Price, 'f', -1, 64)
	}
	return strings.Join([]string{offer.Provider, offer.CountryISO2, offer.Service, priceTier}, "\x00")
}

func bulkItemsForTask(server *rpc.Server, task bulkregistration.Task, offers []bulkregistration.Offer, now time.Time) []bulkregistration.Item {
	offerByID := map[string]bulkregistration.Offer{}
	for _, offer := range offers {
		offerByID[offer.OfferID] = offer
	}
	items := make([]bulkregistration.Item, 0, task.TargetCount)
	for _, selection := range task.Selections {
		offer := offerByID[selection.OfferID]
		for count := 0; count < selection.Quantity; count++ {
			items = append(items, bulkregistration.Item{ItemID: server.IDs().NewID("wabulki_"), TaskID: task.TaskID, Status: bulkregistration.ItemStatusQueued, Provider: offer.Provider, Operator: offer.Operator, OfferID: offer.OfferID, Price: offer.Price, Currency: offer.Currency, CountryISO2: task.CountryISO2, SMSStatus: "QUEUED", CreatedAt: now, UpdatedAt: now})
		}
	}
	return items
}

func bulkOfferFromProvider(offer smsotp.Offer) bulkregistration.Offer {
	return bulkregistration.Offer{OfferID: offer.OfferID, Provider: offer.Provider, CountryISO2: offer.CountryISO2, Service: offer.Service, Price: offer.Price, Currency: offer.Currency, AvailableCount: offer.AvailableCount, PriceTier: offer.PriceTier, Operator: offer.Operator}
}

func smsOfferFromItem(item bulkregistration.Item) smsotp.Offer {
	return smsotp.Offer{OfferID: item.OfferID, Provider: item.Provider, CountryISO2: item.CountryISO2, Service: bulkRegistrationService, Price: item.Price, Currency: item.Currency, Operator: item.Operator}
}

func bulkPhoneTarget(e164 string, countryISO2 string) (*waappPhoneTarget, error) {
	parsed, err := phonenumbers.Parse(e164, countryISO2)
	if err != nil || !phonenumbers.IsValidNumber(parsed) {
		return nil, fmt.Errorf("invalid E.164 number")
	}
	requestedCountry := normalizeBulkCountry(countryISO2)
	actualCountry := normalizeBulkCountry(phonenumbers.GetRegionCodeForNumber(parsed))
	if requestedCountry == "" || actualCountry == "" || actualCountry != requestedCountry {
		return nil, fmt.Errorf("supplier returned a phone number outside the selected country")
	}
	callingCode := strconv.Itoa(int(parsed.GetCountryCode()))
	nationalNumber := strconv.FormatUint(parsed.GetNationalNumber(), 10)
	return &waappPhoneTarget{E164Number: "+" + strconv.Itoa(int(parsed.GetCountryCode())) + nationalNumber, CountryCallingCode: callingCode, CountryIso2: actualCountry}, nil
}

// waappPhoneTarget is deliberately small so bulk worker does not expose this
// implementation detail from its dashboard response schema.
type waappPhoneTarget struct {
	E164Number         string
	CountryCallingCode string
	CountryIso2        string
}

func (m *bulkRegistrationManager) cleanupPendingAccount(item bulkregistration.Item) {
	if item.WAAccountID == "" {
		return
	}
	gateway := &actionGateway{server: m.server, registrationProxy: newRegistrationProxyResolver(m.registrationProxy)}
	_, _ = gateway.cleanupFailedRegistration(context.Background(), map[string]any{"wa_account_id": item.WAAccountID, "verification_request_id": item.VerificationRequestID})
}

func taskCancelRequested(ctx context.Context, server *rpc.Server, taskID string) bool {
	task, err := server.Store().GetTask(ctx, taskID)
	return err == nil && task != nil && (task.Status == bulkregistration.TaskStatusCancelRequested || task.Status == bulkregistration.TaskStatusCanceling)
}

func bulkTaskCounts(items []bulkregistration.Item) (success int, failed int, canceled int, waiting int) {
	for _, item := range items {
		switch item.Status {
		case bulkregistration.ItemStatusRegistered:
			success++
		case bulkregistration.ItemStatusFailed:
			failed++
		case bulkregistration.ItemStatusCancelPending:
			failed++
		case bulkregistration.ItemStatusCanceled, bulkregistration.ItemStatusNumberCanceled:
			canceled++
		default:
			waiting++
		}
	}
	return success, failed, canceled, waiting
}

func normalizeBulkCountry(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func compactBulkError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len(message) > 180 {
		return message[:180]
	}
	return message
}

func safeBulkEventMessage(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return compactBulkError(errors.New(shared.SafeInternalErrorMessage(errors.New(value))))
}

func bulkFailureKind(message string) string {
	message = strings.ToLower(message)
	switch {
	case strings.Contains(message, "reason=blocked"), strings.Contains(message, " blocked"):
		return "wa_blocked"
	case strings.Contains(message, "proxy"):
		return "registration_proxy"
	case strings.Contains(message, "timed out waiting for the sms"):
		return "sms_timeout"
	case strings.Contains(message, "sms activation cancellation"):
		return "sms_cancel_pending"
	case strings.Contains(message, "no_numbers"), strings.Contains(message, "no numbers"):
		return "sms_number_unavailable"
	case strings.Contains(message, "could not read the sms"), strings.Contains(message, "could not prepare the sms"):
		return "sms_provider"
	case strings.Contains(message, "invalid e.164"), strings.Contains(message, "outside the selected country"):
		return "invalid_phone"
	default:
		return "unknown"
	}
}

func bulkFailureReason(value string) string {
	parts := strings.Split(value, ";")
	result := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, bulkCancellationPendingPrefix) || part == "SMS activation cancellation is pending" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return compactBulkError(errors.New(strings.Join(result, "; ")))
}

func bulkCancellationPendingError(failureReason string, cancelErr error) string {
	failureReason = bulkFailureReason(failureReason)
	detail := bulkCancellationPendingPrefix
	if message := compactBulkError(cancelErr); message != "" {
		detail += ": " + message
	}
	if failureReason == "" {
		return detail
	}
	return compactBulkError(errors.New(failureReason + "; " + detail))
}

func waitBulkRegistration(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func bulkJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func bulkMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	bulkJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func bulkError(w http.ResponseWriter, err error) {
	if errors.Is(err, bulkregistration.ErrTaskNotFound) {
		bulkJSON(w, http.StatusNotFound, map[string]string{"error": "bulk registration task not found"})
		return
	}
	bulkJSON(w, http.StatusBadRequest, map[string]string{"error": compactBulkError(errors.New(shared.SafeInternalErrorMessage(err)))})
}
