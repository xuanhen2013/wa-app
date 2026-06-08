import { type FormEvent, useState } from 'react';
import { CheckCircle2, KeyRound, Mail, Send, ShieldCheck } from 'lucide-react';
import { useMutation } from '@tanstack/react-query';
import { Badge, Button, Field, FieldDescription, FieldGroup, FieldLabel, Input } from './ui';
import { AccountSettingsOperationStatus } from '../proto/byte/v/forge/waapp/v1/account_settings';
import type { WaAccountProjection } from './wa-api';
import { requestWaAccountEmailOtp, setWaAccountEmail, setWaTwoFactorAuthSettings, verifyWaAccountEmailOtp } from './wa-api';

type Props = {
  account: WaAccountProjection;
  onDone: (message: string) => void;
  onError: (message: string) => void;
};

export function WaAccountSecurityPanel({ account, onDone, onError }: Props) {
  const [pin, setPin] = useState('');
  const [recoveryEmail, setRecoveryEmail] = useState('');
  const [email, setEmail] = useState('');
  const [idToken, setIdToken] = useState('');
  const [emailOtp, setEmailOtp] = useState('');
  const [lastStatus, setLastStatus] = useState<AccountSettingsOperationStatus | undefined>();
  const handleError = (error: unknown) => onError(error instanceof Error ? error.message : String(error));
  const handleSuccess = (message: string, status?: AccountSettingsOperationStatus) => {
    setLastStatus(status);
    onDone(message);
  };
  const twoFactor = useMutation({
    mutationFn: () => setWaTwoFactorAuthSettings(account, { pin, recovery_email: recoveryEmail }),
    onSuccess: (resp) => { setPin(''); handleSuccess('2FA PIN 设置请求已提交', resp.operation?.status); },
    onError: handleError,
  });
  const emailSet = useMutation({
    mutationFn: () => setWaAccountEmail(account, { email_address: email, google_id_token: idToken }),
    onSuccess: (resp) => { setIdToken(''); handleSuccess('账户邮箱设置请求已提交', resp.operation?.status); },
    onError: handleError,
  });
  const otpRequest = useMutation({
    mutationFn: () => requestWaAccountEmailOtp(account),
    onSuccess: (resp) => handleSuccess('邮箱 OTP 已请求', resp.operation?.status),
    onError: handleError,
  });
  const otpVerify = useMutation({
    mutationFn: () => verifyWaAccountEmailOtp(account, emailOtp),
    onSuccess: (resp) => { setEmailOtp(''); handleSuccess('邮箱 OTP 校验请求已提交', resp.operation?.status); },
    onError: handleError,
  });
  const busy = twoFactor.isPending || emailSet.isPending || otpRequest.isPending || otpVerify.isPending;
  return (
    <section className="grid gap-3 p-4">
      <div className="flex items-center justify-between">
        <div className="grid gap-1"><h3 className="text-sm font-semibold text-foreground">安全 / 邮箱</h3><p className="text-xs text-muted-foreground">直接通过当前活跃登录态执行原子设置请求，不保存 PIN、OTP 或 token。</p></div>
        <Badge variant="outline">{statusLabel(lastStatus)}</Badge>
      </div>
      <form className="rounded-xl border bg-card p-3" onSubmit={(event) => submit(event, twoFactor.mutate)}>
        <div className="mb-3 flex items-center gap-2 text-sm font-medium"><ShieldCheck size={15} /> 设置 2FA PIN</div>
        <FieldGroup>
          <Field><FieldLabel>6 位 PIN</FieldLabel><Input value={pin} onChange={(event) => setPin(event.target.value)} inputMode="numeric" autoComplete="one-time-code" type="password" maxLength={6} disabled={busy} /></Field>
          <Field><FieldLabel>恢复邮箱</FieldLabel><Input value={recoveryEmail} onChange={(event) => setRecoveryEmail(event.target.value)} type="email" placeholder="可选" disabled={busy} /></Field>
          <FieldDescription>PIN 只用于本次请求；恢复邮箱可留空。</FieldDescription>
          <Button type="submit" disabled={busy || pin.length !== 6}><KeyRound size={14} /> 提交 PIN</Button>
        </FieldGroup>
      </form>
      <form className="rounded-xl border bg-card p-3" onSubmit={(event) => submit(event, emailSet.mutate)}>
        <div className="mb-3 flex items-center gap-2 text-sm font-medium"><Mail size={15} /> 设置账户邮箱</div>
        <FieldGroup>
          <Field><FieldLabel>邮箱地址</FieldLabel><Input value={email} onChange={(event) => setEmail(event.target.value)} type="email" disabled={busy} /></Field>
          <Field><FieldLabel>Google ID token</FieldLabel><Input value={idToken} onChange={(event) => setIdToken(event.target.value)} type="password" placeholder="可选" disabled={busy} /></Field>
          <FieldDescription>如服务端需要验证，可继续请求邮箱 OTP。</FieldDescription>
          <Button type="submit" disabled={busy || !email}><Mail size={14} /> 提交邮箱</Button>
        </FieldGroup>
      </form>
      <div className="rounded-xl border bg-card p-3">
        <div className="mb-3 flex items-center gap-2 text-sm font-medium"><Send size={15} /> 邮箱 OTP</div>
        <div className="grid gap-3 sm:grid-cols-[auto_1fr_auto]">
          <Button type="button" variant="outline" disabled={busy} onClick={() => otpRequest.mutate()}><Send size={14} /> 请求 OTP</Button>
          <Input value={emailOtp} onChange={(event) => setEmailOtp(event.target.value)} inputMode="numeric" autoComplete="one-time-code" type="password" maxLength={6} disabled={busy} placeholder="6 位验证码" />
          <Button type="button" disabled={busy || emailOtp.length !== 6} onClick={() => otpVerify.mutate()}><CheckCircle2 size={14} /> 校验 OTP</Button>
        </div>
      </div>
    </section>
  );
}

function submit(event: FormEvent<HTMLFormElement>, run: () => void) {
  event.preventDefault();
  run();
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
