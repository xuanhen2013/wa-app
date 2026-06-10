import { useState } from 'react';
import { CheckCircle2, KeyRound, Search } from 'lucide-react';
import { probeWaPhoneSMS, registerWaPhone, submitWaRegistrationOTP, type WaWorkflowResponse } from './wa-api';
import { WhatsAppIcon } from './wa-brand-icon';
import { waProbeCanStartRegistration, waProbeStatus } from './wa-result-model';
import { WaResultPanel } from './wa-result-panel';
import { resolveWaPhoneTarget, type WaResolvedPhone } from './wa-utils';
import { Alert, AlertDescription, Badge, Button, Field, FieldDescription, FieldGroup, FieldLabel, Input } from './ui';

type ProbeState = { target: WaResolvedPhone; result: WaWorkflowResponse } | null;
type PendingRegistration = { target: WaResolvedPhone; accountID: string; verificationRequestID: string };

type Props = { disabled?: boolean; onChanged: () => void | Promise<void>; onDone: (message: string) => void; onError: (message: string) => void };

export function WaAccountAdd({ disabled, onChanged, onDone, onError }: Props) {
  const [phone, setPhone] = useState('');
  const [countryCallingCode, setCountryCallingCode] = useState('');
  const [probe, setProbe] = useState<ProbeState>(null);
  const [pending, setPending] = useState<PendingRegistration | null>(null);
  const [otp, setOtp] = useState('');
  const [busy, setBusy] = useState(false);
  const samePhone = probeMatchesValues(probe, phone, countryCallingCode);
  const status = waProbeStatus(samePhone ? probe?.result : null);
  const canRegister = samePhone && waProbeCanStartRegistration(probe?.result);

  async function run(action: 'probe' | 'register') {
    const resolved = resolveWaPhoneTarget(phone, countryCallingCode);
    if (!resolved.target) return onError(resolved.error || '请输入手机号和国家拨号码。');
    if (action === 'register' && !canRegister) return onError('请先完成检测，且检测通过后才能发起注册。');
    setBusy(true);
    try {
      if (action === 'probe') setProbe({ target: resolved.target, result: await probeWaPhoneSMS(resolved.target.input) });
      if (action === 'register') {
        const result = await registerWaPhone(resolved.target.input);
        if (result.success === false || result.error_message) throw new Error(result.error_message || result.status || 'WA 注册流程发起失败');
        const accountID = workflowText(result, 'wa_account_id');
        if (accountID) setPending({ target: resolved.target, accountID, verificationRequestID: workflowText(result, 'verification_request_id') });
        setProbe(null);
        setOtp('');
        onDone(accountID ? 'OTP 已发送，请输入验证码' : '注册流程已发起');
        await onChanged();
      }
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }

  async function submitOTP() {
    if (!pending) return onError('没有等待中的注册 OTP。');
    const code = otp.trim();
    if (!code) return onError('请输入 OTP。');
    setBusy(true);
    try {
      const result = await submitWaRegistrationOTP(pending.accountID, code);
      if (result.success === false || result.error_message) throw new Error(result.error_message || result.status || 'OTP 提交失败');
      setOtp('');
      setPending(null);
      onDone('OTP 已提交');
      await onChanged();
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="grid gap-3 rounded-2xl border border-border bg-card p-4 shadow-sm">
      <div className="flex items-center justify-between gap-3">
        <div><h2 className="inline-flex items-center gap-2 text-base font-semibold"><WhatsAppIcon className="size-5" />添加并注册 WAAccount</h2><p className="text-xs text-muted-foreground">先检测手机号/SMS 状态；检测通过后才发起注册并持久化账号。</p></div>
        {canRegister && <Badge variant="default"><CheckCircle2 size={12} /> 可注册</Badge>}
      </div>
      <FieldGroup>
        <div className="grid gap-3 sm:grid-cols-[160px_1fr]">
          <Field><FieldLabel>国家拨号码</FieldLabel><Input placeholder="+1" value={countryCallingCode} onChange={(event) => setCountryCallingCode(event.target.value)} disabled={busy || disabled} /></Field>
          <Field><FieldLabel>手机号</FieldLabel><Input placeholder="4155550123" value={phone} onChange={(event) => setPhone(event.target.value)} disabled={busy || disabled} /></Field>
        </div>
        <FieldDescription>填写国家拨号码和手机号；代理未配置时服务端会尝试直连。</FieldDescription>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" disabled={busy || disabled} onClick={() => void run('probe')}><Search size={14} /> 检测</Button>
          <Button type="button" disabled={busy || disabled || Boolean(pending) || !canRegister} onClick={() => void run('register')}>发起注册</Button>
          {probe && !samePhone && <Badge variant="outline">号码已变化，请重新检测</Badge>}
        </div>
      </FieldGroup>
      <Alert><AlertDescription>{pending ? 'OTP 已发送，请在本页输入验证码完成注册。' : canRegister ? '检测通过，可以点击“发起注册”。' : '检测通过前不会持久化 WAAccount。'}</AlertDescription></Alert>
      {pending && <PendingOtpForm pending={pending} otp={otp} busy={busy} setOtp={setOtp} submitOTP={submitOTP} />}
      {(probe || busy) && <WaResultPanel title="检测结果" phone={samePhone ? probe?.target.e164 || '' : ''} result={samePhone ? probe?.result || null : null} loading={busy} />}
      {samePhone && status.requestFailed && <p className="text-xs text-destructive">{status.failureReason || '检测失败'}</p>}
    </section>
  );
}

function probeMatchesValues(probe: ProbeState, phone: string, countryCallingCode: string) {
  if (!probe) return false;
  return resolveWaPhoneTarget(phone, countryCallingCode).target?.e164 === probe.target.e164;
}


function PendingOtpForm({ pending, otp, busy, setOtp, submitOTP }: { pending: PendingRegistration; otp: string; busy: boolean; setOtp: (value: string) => void; submitOTP: () => void }) {
  return (
    <section className="grid gap-2 rounded-xl border border-border p-3">
      <h3 className="inline-flex items-center gap-2 text-sm font-semibold"><KeyRound size={15} />输入注册 OTP</h3>
      <div className="flex gap-2">
        <Input value={otp} onChange={(event) => setOtp(event.target.value)} inputMode="numeric" autoComplete="one-time-code" type="password" placeholder="验证码" disabled={busy} />
        <Button type="button" disabled={busy || !otp.trim()} onClick={() => submitOTP()}>提交</Button>
      </div>
      <FieldDescription>{pending.target.e164}{pending.verificationRequestID ? ` · ${pending.verificationRequestID}` : ''}</FieldDescription>
    </section>
  );
}

function workflowText(result: WaWorkflowResponse, key: keyof WaWorkflowResponse) {
  const value = result[key];
  return typeof value === 'string' ? value.trim() : '';
}
