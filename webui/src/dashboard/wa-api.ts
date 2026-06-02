import { ACCOUNT_PAGE_SIZE, api, fetchAccountList } from '@byte-v-forge/common-ui';
import type { ListAccountOtpMessagesResponse } from '../proto/byte/v/forge/waapp/v1/extraction';
import type { GetLongConnectionStatusResponse, LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { CreateWAAccountResponse, DeleteWAAccountResponse, ListWAAccountsResponse, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';

export type WaPhoneInput = {
  workspace_id: string;
  region: string;
  phone: string;
  e164_number: string;
  country_calling_code: string;
  country_iso2: string;
};

export type WaWorkflowResponse = {
  success?: boolean;
  passed?: boolean;
  request_failed?: boolean;
  status?: string;
  error_message?: string;
  phone_status?: Record<string, unknown>;
  account_probe?: Record<string, unknown>;
  sms_probe?: Record<string, unknown>;
  phone?: Record<string, unknown>;
  proxy?: Record<string, unknown>;
  registration?: Record<string, unknown>;
  login_state?: Record<string, unknown>;
  check?: Record<string, unknown>;
};

export type WaConnectionState = LongConnectionState;
export type WaConnectionFilters = {
  login_state_id?: string;
  wa_account_id?: string;
  client_profile_id?: string;
  registered_identity_id?: string;
};
export type WaAccountProjection = WAAccount;

export type WaHealthResponse = {
  ok: boolean;
  n8n_webhook_configured: boolean;
  workflows: Array<{ key: string; label: string; webhook_path: string }>;
};

export const waKeys = {
  health: ['wa', 'health'] as const,
  accounts: (workspaceId: string) => ['wa', 'accounts', workspaceId] as const,
  otpMessages: (workspaceId: string, waAccountId: string) => ['wa', 'otp-messages', workspaceId, waAccountId] as const,
  connections: (workspaceId: string, filters: WaConnectionFilters = {}) => ['wa', 'connections', workspaceId, filters.login_state_id || '', filters.wa_account_id || '', filters.client_profile_id || '', filters.registered_identity_id || ''] as const
};

export function getWaHealth() {
  return api<WaHealthResponse>('/api/wa/health');
}

export function getWaConnections(workspaceId: string, filters: WaConnectionFilters = {}) {
  const params = new URLSearchParams({ workspace_id: workspaceId || 'default' });
  for (const [key, value] of Object.entries(filters)) {
    if (value) params.set(key, value);
  }
  return api<GetLongConnectionStatusResponse>(`/api/wa/long-connections?${params.toString()}`);
}

export function getWaAccounts(workspaceId: string, cursor = '') {
  return fetchAccountList<WAAccount, ListWAAccountsResponse>({
    path: '/api/wa/accounts',
    cursor,
    limit: ACCOUNT_PAGE_SIZE,
    params: { workspace_id: workspaceId || 'default' }
  });
}

export function getWaAccountOtpMessages(workspaceId: string, waAccountId: string, cursor = '') {
  const params = new URLSearchParams({
    workspace_id: workspaceId || 'default',
    wa_account_id: waAccountId,
    limit: '20'
  });
  if (cursor) params.set('cursor', cursor);
  return api<ListAccountOtpMessagesResponse>(`/api/wa/account-otp-messages?${params.toString()}`);
}

export async function createWaAccount(input: { phone: string; country_calling_code: string }, workspaceId = 'default') {
  const resp = await api<CreateWAAccountResponse>('/api/wa/accounts', {
    method: 'POST',
    body: JSON.stringify({ workspace_id: workspaceId, phone: input.phone, country_calling_code: input.country_calling_code })
  });
  if (resp.error?.message) throw new Error(resp.error.message);
  if (!resp.account) throw new Error('WAAccount response is empty');
  return resp.account;
}

export async function deleteWaAccount(account: WAAccount | string, workspaceId = 'default') {
  const accountID = typeof account === 'string' ? account : account.account?.key?.account_id || '';
  if (!accountID) throw new Error('wa_account_id is required');
  const params = new URLSearchParams({ workspace_id: workspaceId || 'default' });
  const resp = await api<DeleteWAAccountResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}?${params.toString()}`, { method: 'DELETE' });
  if (!resp.success || resp.error?.message) throw new Error(resp.error?.message || 'delete WAAccount failed');
  return resp;
}

export function probeWaPhoneSMS(input: WaPhoneInput) {
  return api<WaWorkflowResponse>('/api/wa/phone/sms-probe', { method: 'POST', body: JSON.stringify(input) });
}

export function probeWaAccount(account: WAAccount, workspaceId = 'default') {
  return api<WaWorkflowResponse>('/api/wa/phone/sms-probe', { method: 'POST', body: JSON.stringify(waAccountActionPayload(account, workspaceId)) });
}

export function registerWaAccount(account: WAAccount, workspaceId = 'default') {
  return api<WaWorkflowResponse>('/api/wa/register', { method: 'POST', body: JSON.stringify(waAccountActionPayload(account, workspaceId)) });
}

export function submitWaRegistrationOTP(account: WAAccount, otp: string, workspaceId = 'default') {
  const payload = waAccountActionPayload(account, workspaceId);
  return api<WaWorkflowResponse>('/api/wa/actions/registration/resume-otp', {
    method: 'POST',
    body: JSON.stringify({ workspace_id: payload.workspace_id, wa_account_id: payload.wa_account_id, otp }),
  });
}

export function checkWaLoginState(input: { workspace_id?: string; login_state_id?: string; registered_identity_id?: string; wa_account_id?: string; client_profile_id?: string; remote_timeout_seconds?: number }) {
  return api<WaWorkflowResponse>('/api/wa/login-state-check', { method: 'POST', body: JSON.stringify(input) });
}

function waAccountActionPayload(account: WAAccount, workspaceId: string) {
  const phone = account.phone;
  if (!phone?.e164_number || !phone.country_calling_code) throw new Error('WAAccount phone is incomplete');
  return {
    workspace_id: account.workspace_id || workspaceId,
    wa_account_id: account.account?.key?.account_id || '',
    phone,
  };
}
