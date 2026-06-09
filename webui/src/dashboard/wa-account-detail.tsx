import { useState } from 'react';
import { KeyRound, Shield, Trash2 } from 'lucide-react';
import { WAAccountStatus } from '../proto/byte/v/forge/waapp/v1/profile';
import type { ClientProfile, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { submitWaRegistrationOTP, waAccountID, waAccountTitle } from './wa-api';
import { WaAccountSecurityPanel } from './wa-account-security';
import { WaDeviceFingerprintPanel } from './wa-device-fingerprint';
import { Badge, Button, Input } from './ui';

type Props = { account: WAAccount; profiles: ClientProfile[]; profilesLoading: boolean; busy: boolean; onDelete: (account: WAAccount) => void; onDone: (message: string) => void; onError: (message: string) => void };

export function WaAccountDetail({ account, profiles, profilesLoading, busy, onDelete, onDone, onError }: Props) {
  return (
    <section className="grid gap-4">
      <header className="flex items-start justify-between gap-3">
        <div><h2 className="text-base font-semibold">{waAccountTitle(account)}</h2><p className="truncate font-mono text-xs text-muted-foreground">{waAccountID(account)}</p></div>
        <Badge variant="outline">{account.status || 'UNKNOWN'}</Badge>
      </header>
      {isRegistrationPending(account) && <ManualOtpSubmit account={account} busy={busy} onDone={onDone} onError={onError} />}
      <section className="rounded-xl border border-border p-3"><h3 className="mb-2 text-sm font-semibold">基础信息</h3><InfoGrid account={account} /></section>
      <details className="rounded-xl border border-border p-3" open><summary className="cursor-pointer text-sm font-semibold">设备指纹</summary><div className="mt-3"><WaDeviceFingerprintPanel profiles={profiles} loading={profilesLoading} /></div></details>
      <details className="rounded-xl border border-border p-3"><summary className="inline-flex cursor-pointer items-center gap-2 text-sm font-semibold"><Shield size={15} />安全设置</summary><div className="mt-3"><WaAccountSecurityPanel account={account} onDone={onDone} onError={onError} /></div></details>
      <details className="rounded-xl border border-destructive/30 p-3"><summary className="cursor-pointer text-sm font-semibold text-destructive">危险操作</summary><div className="mt-3"><Button variant="destructive" disabled={busy} onClick={() => onDelete(account)}><Trash2 size={14} />删除账号</Button></div></details>
    </section>
  );
}

function isRegistrationPending(account: WAAccount) {
  return account.status === WAAccountStatus.WA_ACCOUNT_STATUS_PENDING_REGISTRATION;
}

function ManualOtpSubmit({ account, busy, onDone, onError }: { account: WAAccount; busy: boolean; onDone: (message: string) => void; onError: (message: string) => void }) {
  const [otp, setOtp] = useState('');
  async function submit() {
    try {
      const resp = await submitWaRegistrationOTP(account, otp);
      if (resp.error_message || resp.success === false) throw new Error(resp.error_message || 'OTP 提交失败');
      setOtp('');
      onDone('OTP 已提交');
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    }
  }
  return <section className="grid gap-2 rounded-xl border border-border p-3"><h3 className="inline-flex items-center gap-2 text-sm font-semibold"><KeyRound size={15} />提交注册 OTP</h3><div className="flex gap-2"><Input value={otp} onChange={(event) => setOtp(event.target.value)} inputMode="numeric" autoComplete="one-time-code" type="password" placeholder="验证码" /><Button disabled={busy || !otp.trim()} onClick={() => void submit()}>提交</Button></div></section>;
}

function InfoGrid({ account }: { account: WAAccount }) {
  const rows = [
    ['账号 ID', waAccountID(account)],
    ['国家', account.phone?.country_iso2 || '-'],
    ['拨号码', account.phone?.country_calling_code || '-'],
    ['更新时间', formatTime(account.audit?.updated_at)],
  ];
  return <dl className="grid gap-1.5 sm:grid-cols-2">{rows.map(([label, value]) => <div key={label} className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-2 rounded-md bg-muted/40 px-2 py-1.5"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="truncate font-mono text-xs">{value}</dd></div>)}</dl>;
}

function formatTime(value?: string) {
  if (!value) return '-';
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? value : time.toLocaleString();
}
