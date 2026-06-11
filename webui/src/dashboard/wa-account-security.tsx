import { type FormEvent, type ReactNode, useEffect, useState } from 'react';
import { CheckCircle2, KeyRound, Mail, Pencil, Send, ShieldCheck, X } from 'lucide-react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { AccountSettingsOperationStatus } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { WaAccountProjection } from './wa-api';
import { getWaTwoFactorAuthStatus, requestWaAccountEmailOtp, setWaAccountEmail, setWaTwoFactorAuthSettings, verifyWaAccountEmailOtp, waAccountID, waKeys } from './wa-api';
import { Badge, type BadgeVariant, Button, Field, FieldGroup, FieldLabel, Input } from './ui';

type Props = { account: WaAccountProjection; onDone: (message: string) => void; onError: (message: string) => void };
type TwoFactorStatusView = { isPending: boolean; isError: boolean; data?: { status?: { configured?: boolean; email_configured?: boolean } } };

export function WaAccountSecurityPanel({ account, onDone, onError }: Props) {
  const [pin, setPin] = useState('');
  const [email, setEmail] = useState('');
  const [emailOtp, setEmailOtp] = useState('');
  const [emailOtpVisible, setEmailOtpVisible] = useState(false);
  const [pinEditing, setPinEditing] = useState(false);
  const [emailEditing, setEmailEditing] = useState(false);
  const [lastStatus, setLastStatus] = useState<AccountSettingsOperationStatus | undefined>();
  const handleError = (error: unknown) => onError(error instanceof Error ? error.message : String(error));
  const handleSuccess = (message: string, status?: AccountSettingsOperationStatus) => { setLastStatus(status); onDone(message); };
  const accountID = waAccountID(account);
  const twoFactorStatus = useQuery({
    queryKey: waKeys.twoFactorStatus(accountID),
    queryFn: () => getWaTwoFactorAuthStatus(account),
    enabled: Boolean(accountID),
    staleTime: 30_000,
  });
  const pinConfigured = twoFactorConfigured(twoFactorStatus);
  const emailConfigured = twoFactorEmailConfigured(twoFactorStatus);
  const pinAction = pinConfigured ? '修改 2FA PIN' : '设置 2FA PIN';
  const emailAction = emailConfigured ? '修改账户邮箱' : '设置账户邮箱';
  const twoFactor = useMutation({
    mutationFn: () => setWaTwoFactorAuthSettings(account, pin),
    onSuccess: (resp) => {
      setPin('');
      setPinEditing(false);
      void twoFactorStatus.refetch();
      handleSuccess(pinConfigured ? '2FA PIN 修改请求已提交' : '2FA PIN 设置请求已提交', resp.operation?.status);
    },
    onError: handleError,
  });
  const emailSet = useMutation({
    mutationFn: () => setWaAccountEmail(account, { email_address: email }),
    onSuccess: (resp) => {
      const status = resp.operation?.status;
      setEmailOtpVisible(shouldCollectEmailOtpAfterSet(status));
      setEmailEditing(false);
      if (status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED) setEmailOtp('');
      void twoFactorStatus.refetch();
      handleSuccess(emailConfigured ? '账户邮箱修改请求已提交' : '账户邮箱设置请求已提交', status);
    },
    onError: handleError,
  });
  const otpRequest = useMutation({
    mutationFn: () => requestWaAccountEmailOtp(account),
    onSuccess: (resp) => {
      setEmailOtpVisible(true);
      handleSuccess('邮箱 OTP 已请求', resp.operation?.status);
    },
    onError: handleError,
  });
  const otpVerify = useMutation({
    mutationFn: () => verifyWaAccountEmailOtp(account, emailOtp),
    onSuccess: (resp) => {
      const status = resp.operation?.status;
      setEmailOtp('');
      setEmailOtpVisible(shouldShowEmailOtp(status));
      void twoFactorStatus.refetch();
      handleSuccess('邮箱 OTP 校验请求已提交', status);
    },
    onError: handleError,
  });
  const busy = twoFactor.isPending || emailSet.isPending || otpRequest.isPending || otpVerify.isPending;
  const handleEmailChange = (value: string) => { setEmail(value); setEmailOtp(''); setEmailOtpVisible(false); };
  const pinFormVisible = statusReady(twoFactorStatus) && (!pinConfigured || pinEditing);
  const emailFormVisible = statusReady(twoFactorStatus) && ((!emailConfigured && !emailOtpVisible) || emailEditing);
  useEffect(() => {
    setPin('');
    setEmail('');
    setEmailOtp('');
    setEmailOtpVisible(false);
    setPinEditing(false);
    setEmailEditing(false);
  }, [accountID]);
  return (
    <section className="grid gap-4">
      {lastStatus !== undefined ? <div className="flex items-center justify-end"><Badge variant="outline">{statusLabel(lastStatus)}</Badge></div> : null}
      <div className="grid gap-4 lg:grid-cols-2">
        <section className="grid gap-3">
          <SettingHeader icon={<ShieldCheck size={15} />} title={pinAction} badge={<Badge variant={twoFactorBadgeVariant(twoFactorStatus)}>{twoFactorStatusLabel(twoFactorStatus)}</Badge>} canEdit={pinConfigured && !pinEditing && !busy} onEdit={() => setPinEditing(true)} />
          {pinFormVisible ? <PinForm pin={pin} busy={busy} configured={pinConfigured} onPinChange={setPin} onCancel={() => { setPin(''); setPinEditing(false); }} onSubmit={(event) => submit(event, twoFactor.mutate)} /> : null}
        </section>
        <section className="grid gap-3">
          <SettingHeader icon={<Mail size={15} />} title={emailAction} badge={<Badge variant={emailBadgeVariant(twoFactorStatus)}>{emailStatusLabel(twoFactorStatus)}</Badge>} canEdit={emailConfigured && !emailEditing && !busy} onEdit={() => setEmailEditing(true)} />
          {emailFormVisible ? <EmailForm email={email} busy={busy} configured={emailConfigured} onEmailChange={handleEmailChange} onCancel={() => { setEmail(''); setEmailEditing(false); }} onSubmit={(event) => submit(event, emailSet.mutate)} /> : null}
        </section>
        {emailOtpVisible && (
          <div className="grid gap-3 border-t border-border pt-5 lg:col-span-2">
            <div className="flex items-center gap-2 text-sm font-medium"><Send size={15} />邮箱 OTP</div>
            <div className="grid gap-3 sm:grid-cols-[auto_1fr_auto]">
              <Button type="button" variant="outline" disabled={busy} onClick={() => otpRequest.mutate()}><Send size={14} />请求 OTP</Button>
              <Input value={emailOtp} onChange={(event) => setEmailOtp(event.target.value)} inputMode="numeric" autoComplete="one-time-code" type="password" maxLength={6} disabled={busy} placeholder="6 位验证码" />
              <Button type="button" disabled={busy || emailOtp.length !== 6} onClick={() => otpVerify.mutate()}><CheckCircle2 size={14} />校验 OTP</Button>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}

function SettingHeader({ icon, title, badge, canEdit, onEdit }: { icon: ReactNode; title: string; badge: ReactNode; canEdit: boolean; onEdit: () => void }) {
  return <div className="flex items-center justify-between gap-2"><div className="inline-flex items-center gap-2 text-sm font-medium">{icon}{title}{badge}</div>{canEdit ? <Button size="icon" variant="ghost" type="button" title={title} aria-label={title} onClick={onEdit}><Pencil size={16} /></Button> : null}</div>;
}

function PinForm({ pin, busy, configured, onPinChange, onCancel, onSubmit }: { pin: string; busy: boolean; configured: boolean; onPinChange: (value: string) => void; onCancel: () => void; onSubmit: (event: FormEvent<HTMLFormElement>) => void }) {
  return <form className="grid gap-3" onSubmit={onSubmit}><FieldGroup><Field><FieldLabel>{configured ? '新 6 位 PIN' : '6 位 PIN'}</FieldLabel><Input value={pin} onChange={(event) => onPinChange(event.target.value)} inputMode="numeric" autoComplete="one-time-code" type="password" maxLength={6} disabled={busy} /></Field>{configured ? <Button type="button" variant="ghost" size="icon" disabled={busy} title="取消修改" aria-label="取消修改" onClick={onCancel}><X size={16} /></Button> : null}<Button type="submit" disabled={busy || pin.length !== 6}><KeyRound size={14} />{configured ? '修改 PIN' : '设置 PIN'}</Button></FieldGroup></form>;
}

function EmailForm({ email, busy, configured, onEmailChange, onCancel, onSubmit }: { email: string; busy: boolean; configured: boolean; onEmailChange: (value: string) => void; onCancel: () => void; onSubmit: (event: FormEvent<HTMLFormElement>) => void }) {
  return <form className="grid gap-3" onSubmit={onSubmit}><FieldGroup><Field><FieldLabel>{configured ? '新邮箱地址' : '邮箱地址'}</FieldLabel><Input value={email} onChange={(event) => onEmailChange(event.target.value)} type="email" disabled={busy} placeholder={configured ? '新邮箱地址' : '邮箱地址'} /></Field>{configured ? <Button type="button" variant="ghost" size="icon" disabled={busy} title="取消修改" aria-label="取消修改" onClick={onCancel}><X size={16} /></Button> : null}<Button type="submit" disabled={busy || !email}><Mail size={14} />{configured ? '修改邮箱' : '设置邮箱'}</Button></FieldGroup></form>;
}

function submit(event: FormEvent<HTMLFormElement>, run: () => void) { event.preventDefault(); run(); }

function statusReady(query: TwoFactorStatusView) {
  return !query.isPending && !query.isError;
}

function shouldCollectEmailOtpAfterSet(status?: AccountSettingsOperationStatus) {
  return status !== AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED
    && status !== AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED;
}

function shouldShowEmailOtp(status?: AccountSettingsOperationStatus) {
  return status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_NEEDS_VERIFICATION
    || status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_WAITING
    || status === AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_CODE_MISMATCH;
}

function twoFactorStatusLabel(query: TwoFactorStatusView) {
  if (query.isPending) return '读取中';
  if (query.isError) return '读取失败';
  return query.data?.status?.configured ? '已配置' : '未配置';
}

function emailStatusLabel(query: TwoFactorStatusView) {
  if (query.isPending) return '读取中';
  if (query.isError) return '读取失败';
  return query.data?.status?.email_configured ? '已配置' : '未配置';
}

function twoFactorBadgeVariant(query: TwoFactorStatusView): BadgeVariant {
  if (query.isError) return 'destructive';
  return query.data?.status?.configured ? 'default' : 'outline';
}

function emailBadgeVariant(query: TwoFactorStatusView): BadgeVariant {
  if (query.isError) return 'destructive';
  return query.data?.status?.email_configured ? 'default' : 'outline';
}

function twoFactorConfigured(query: TwoFactorStatusView) {
  return Boolean(query.data?.status?.configured);
}

function twoFactorEmailConfigured(query: TwoFactorStatusView) {
  return Boolean(query.data?.status?.email_configured);
}

function statusLabel(status?: AccountSettingsOperationStatus) {
  switch (status) {
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_NEEDS_VERIFICATION: return '待邮箱验证';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_WAITING: return '等待 OTP';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED: return '已验证';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_CODE_MISMATCH: return '验证码不匹配';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED: return '已拒绝';
    case AccountSettingsOperationStatus.ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED: return '已受理';
    default: return '未执行';
  }
}
