import { useState } from 'react';
import { CheckCircle2, Search } from 'lucide-react';
import { probeWaPhoneSMS, registerWaPhone, type WaWorkflowResponse } from './wa-api';
import { WhatsAppIcon } from './wa-brand-icon';
import { waProbeCanStartRegistration, waProbeStatus } from './wa-result-model';
import { WaResultPanel } from './wa-result-panel';
import { resolveWaPhoneTarget, type WaResolvedPhone } from './wa-utils';
import { Alert, AlertDescription, Badge, Button, Field, FieldDescription, FieldGroup, FieldLabel, Input } from './ui';

type ProbeState = { target: WaResolvedPhone; result: WaWorkflowResponse } | null;

type Props = { disabled?: boolean; onCreated: () => void | Promise<void>; onError: (message: string) => void };

export function WaAccountAdd({ disabled, onCreated, onError }: Props) {
  const [phone, setPhone] = useState('');
  const [countryCallingCode, setCountryCallingCode] = useState('');
  const [probe, setProbe] = useState<ProbeState>(null);
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
        setProbe(null);
        await onCreated();
      }
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
          <Button type="button" disabled={busy || disabled || !canRegister} onClick={() => void run('register')}>发起注册</Button>
          {probe && !samePhone && <Badge variant="outline">号码已变化，请重新检测</Badge>}
        </div>
      </FieldGroup>
      <Alert><AlertDescription>{canRegister ? '检测通过，可以点击“发起注册”。发码成功后可在详情页手动提交 OTP。' : '检测通过前不会持久化 WAAccount。'}</AlertDescription></Alert>
      {(probe || busy) && <WaResultPanel title="检测结果" phone={samePhone ? probe?.target.e164 || '' : ''} result={samePhone ? probe?.result || null : null} loading={busy} />}
      {samePhone && status.requestFailed && <p className="text-xs text-destructive">{status.failureReason || '检测失败'}</p>}
    </section>
  );
}

function probeMatchesValues(probe: ProbeState, phone: string, countryCallingCode: string) {
  if (!probe) return false;
  return resolveWaPhoneTarget(phone, countryCallingCode).target?.e164 === probe.target.e164;
}
