package app

import (
	"context"
	"errors"
	"log"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
)

const pendingRegistrationCleanupPageLimit = 100

// markWAAccountTransferredOut 在账号被接管/转出(chatd device_removed/replaced 或 device_logout)时,
// 把账号级状态置为 TRANSFERRED_OUT,使仪表盘账号资料不再显示"正常"。账号不存在或已是该态则跳过;
// 再次注册到本端会经注册流回到 ACTIVE。
func (s *serverCore) markWAAccountTransferredOut(ctx context.Context, waAccountID string) {
	if s == nil || strings.TrimSpace(waAccountID) == "" {
		return
	}
	account, err := s.getWAAccount(ctx, waAccountID)
	if err != nil || account == nil {
		return
	}
	if waAccountStatus(account) == waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_TRANSFERRED_OUT {
		return
	}
	if _, err := s.saveWAAccount(ctx, withWAAccountStatus(account, waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_TRANSFERRED_OUT, s.clock.Now())); err != nil {
		log.Printf("WA mark account transferred out failed: wa_account=%s error=%v", waAccountID, sanitizeLogError(err))
	}
}

func (s *serverCore) saveWAAccount(ctx context.Context, account *waappv1.WAAccount) (*waappv1.WAAccount, error) {
	if account == nil {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "WA account is required", false)
	}
	accountID, err := requireWAAccountID(account.GetWaAccountId())
	if err != nil {
		return nil, err
	}
	account.WaAccountId = accountID
	account.DisplayName = strings.TrimSpace(account.GetDisplayName())
	account.Phone = normalizePhone(account.GetPhone())
	account.Status = normalizeWAAccountStatus(account.GetStatus())
	if err := s.store.SaveWAAccount(ctx, account); err != nil {
		return nil, err
	}
	return s.store.GetWAAccount(ctx, accountID)
}

func (s *serverCore) getWAAccount(ctx context.Context, accountID string) (*waappv1.WAAccount, error) {
	accountID, err := requireWAAccountID(accountID)
	if err != nil {
		return nil, err
	}
	account, err := s.store.GetWAAccount(ctx, accountID)
	if isWAAccountNotFound(err) {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found", false)
	}
	return account, err
}

func (s *serverCore) listWAAccounts(ctx context.Context, cursor string, limit int) ([]*waappv1.WAAccount, string, error) {
	return s.store.ListWAAccounts(ctx, strings.TrimSpace(cursor), limit)
}

func (s *serverCore) deleteWAAccount(ctx context.Context, accountID string) (bool, error) {
	accountID, err := requireWAAccountID(accountID)
	if err != nil {
		return false, err
	}
	err = s.store.DeleteWAAccount(ctx, accountID)
	if isWAAccountNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *serverCore) deletePendingRegistrationWAAccounts(ctx context.Context) (int, error) {
	cursor := ""
	deleted := 0
	for {
		accounts, nextCursor, err := s.listWAAccounts(ctx, cursor, pendingRegistrationCleanupPageLimit)
		if err != nil {
			return deleted, err
		}
		for _, account := range accounts {
			if waAccountStatus(account) != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION {
				continue
			}
			accountID := waAccountID(account)
			if accountID == "" {
				continue
			}
			s.deleteRegistrationOTPWaitForAccount(ctx, accountID)
			found, err := s.deleteWAAccount(ctx, accountID)
			if err != nil {
				return deleted, err
			}
			if found {
				deleted++
			}
		}
		if nextCursor == "" {
			return deleted, nil
		}
		cursor = nextCursor
	}
}

func (s *serverCore) deleteRegistrationOTPWaitForAccount(ctx context.Context, accountID string) {
	if s == nil || s.runtime == nil || strings.TrimSpace(accountID) == "" {
		return
	}
	gateway := &actionGateway{server: s.facade}
	wait, err := gateway.loadRegistrationOTPWait(ctx, accountID, "")
	if err != nil {
		_ = s.runtime.DeleteTransientState(ctx, registrationOTPWaitAccountKey(accountID))
		return
	}
	_ = gateway.deleteRegistrationOTPWait(ctx, wait)
}

func isWAAccountNotFound(err error) bool {
	var appErr *shared.AppError
	return errors.As(err, &appErr) && appErr.Code == waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND
}
