import type { RequestAccountEmailOtpResponse, SetAccountEmailResponse, SetTwoFactorAuthSettingsResponse, VerifyAccountEmailOtpResponse } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { ListWAContactsResponse } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { ListAccountOtpMessagesResponse } from '../proto/byte/v/forge/waapp/v1/extraction';
import type { GetLongConnectionStatusResponse, ListAccountMessagesResponse, LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { DeleteWAAccountResponse, ListClientProfilesResponse, ListWAAccountsResponse, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';

export const ACCOUNT_PAGE_SIZE = 100;

export type WaPhoneInput = { region: string; phone: string; e164_number: string; country_calling_code: string; country_iso2: string };
export type WaWorkflowResponse = { success?: boolean; passed?: boolean; request_failed?: boolean; status?: string; error_message?: string; phone_status?: Record<string, unknown>; account_probe?: Record<string, unknown>; sms_probe?: Record<string, unknown>; phone?: Record<string, unknown>; proxy?: Record<string, unknown>; registration?: Record<string, unknown>; login_state?: Record<string, unknown>; check?: Record<string, unknown> };
export type WaConnectionState = LongConnectionState;
export type WaConnectionFilters = { login_state_id?: string; wa_account_id?: string; client_profile_id?: string; registered_identity_id?: string };
export type WaAccountProjection = WAAccount;

export const waKeys = {
  accounts: () => ['wa', 'accounts'] as const,
  profiles: (waAccountId: string) => ['wa', 'profiles', waAccountId] as const,
  messages: (waAccountId: string) => ['wa', 'messages', waAccountId] as const,
  contacts: (waAccountId: string) => ['wa', 'contacts', waAccountId] as const,
  otpMessages: (waAccountId: string) => ['wa', 'otp-messages', waAccountId] as const,
  connections: (filters: WaConnectionFilters = {}) => ['wa', 'connections', filters.login_state_id || '', filters.wa_account_id || '', filters.client_profile_id || '', filters.registered_identity_id || ''] as const,
};

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, { ...init, headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) } });
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
  return resp.json() as Promise<T>;
}

export function getWaConnections(filters: WaConnectionFilters = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) if (value) params.set(key, value);
  return api<GetLongConnectionStatusResponse>(`/api/wa/long-connections${params.size ? `?${params}` : ''}`);
}

export function getWaAccounts(cursor = '') {
  const params = new URLSearchParams({ limit: String(ACCOUNT_PAGE_SIZE) });
  if (cursor) params.set('cursor', cursor);
  return api<ListWAAccountsResponse>(`/api/wa/accounts?${params}`);
}

export function getWaAccountOtpMessages(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '20' });
  if (cursor) params.set('cursor', cursor);
  return api<ListAccountOtpMessagesResponse>(`/api/wa/account-otp-messages?${params}`);
}

export function getWaClientProfiles(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '20' });
  if (cursor) params.set('cursor', cursor);
  return api<ListClientProfilesResponse>(`/api/wa/client-profiles?${params}`);
}

export function getWaMessages(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '200', include_sensitive_text: 'true' });
  if (cursor) params.set('cursor', cursor);
  return api<ListAccountMessagesResponse>(`/api/wa/messages?${params}`);
}

export function getWaContacts(waAccountId: string, cursor = '') {
  const params = new URLSearchParams({ wa_account_id: waAccountId, limit: '500' });
  if (cursor) params.set('cursor', cursor);
  return api<ListWAContactsResponse>(`/api/wa/contacts?${params}`);
}

export async function deleteWaAccount(account: WAAccount | string) {
  const accountID = typeof account === 'string' ? account : waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  const resp = await api<DeleteWAAccountResponse>(`/api/wa/accounts/${encodeURIComponent(accountID)}`, { method: 'DELETE' });
  if (!resp.success || resp.error?.message) throw new Error(resp.error?.message || 'delete WAAccount failed');
  return resp;
}

export const probeWaPhoneSMS = (input: WaPhoneInput) => api<WaWorkflowResponse>('/api/wa/phone/sms-probe', { method: 'POST', body: JSON.stringify(input) });
export const registerWaPhone = (input: WaPhoneInput) => api<WaWorkflowResponse>('/api/wa/register', { method: 'POST', body: JSON.stringify(input) });
export const checkWaLoginState = (input: { login_state_id?: string; registered_identity_id?: string; wa_account_id?: string; client_profile_id?: string; remote_timeout_seconds?: number }) => api<WaWorkflowResponse>('/api/wa/login-state-check', { method: 'POST', body: JSON.stringify(input) });

export function submitWaRegistrationOTP(account: WAAccount, otp: string) {
  return api<WaWorkflowResponse>('/api/wa/actions/registration/resume-otp', { method: 'POST', body: JSON.stringify({ wa_account_id: waAccountID(account), otp }) });
}

export async function setWaTwoFactorAuthSettings(account: WAAccount, input: { pin: string; recovery_email?: string }) {
  return requireAccountSettingsResponse(await api<SetTwoFactorAuthSettingsResponse>('/api/wa/account-settings/2fa', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), pin: input.pin, recovery_email: input.recovery_email || '' }) }));
}
export async function setWaAccountEmail(account: WAAccount, input: { email_address: string; google_id_token?: string }) {
  return requireAccountSettingsResponse(await api<SetAccountEmailResponse>('/api/wa/account-settings/email', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), email_address: input.email_address, google_id_token: input.google_id_token || '' }) }));
}
export async function requestWaAccountEmailOtp(account: WAAccount) {
  return requireAccountSettingsResponse(await api<RequestAccountEmailOtpResponse>('/api/wa/account-settings/email/otp/request', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), locale_language: 'en', locale_country: 'US' }) }));
}
export async function verifyWaAccountEmailOtp(account: WAAccount, code: string) {
  return requireAccountSettingsResponse(await api<VerifyAccountEmailOtpResponse>('/api/wa/account-settings/email/otp/verify', { method: 'POST', body: JSON.stringify({ ...waAccountSettingsPayload(account), code }) }));
}

export const waAccountID = (account?: WAAccount) => account?.wa_account_id || '';
export const waAccountTitle = (account?: WAAccount) => account?.phone?.e164_number || waAccountID(account) || '-';

function waAccountSettingsPayload(account: WAAccount) {
  const accountID = waAccountID(account);
  if (!accountID) throw new Error('wa_account_id is required');
  return { wa_account_id: accountID };
}
function requireAccountSettingsResponse<T extends { error?: { message?: string }; operation?: { error?: { message?: string } } }>(resp: T) {
  const message = resp.error?.message || resp.operation?.error?.message;
  if (message) throw new Error(message);
  return resp;
}
