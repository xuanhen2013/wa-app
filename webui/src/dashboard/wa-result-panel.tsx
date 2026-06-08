import type { WaWorkflowResponse } from './wa-api';
import { booleanLabel, methodStateLabel, oldDeviceLabel, smsLabel } from './wa-result-labels';
import { metaItems, outcomeMeta, waProbeStatus, type WaProbeStatus } from './wa-result-model';
import { Badge, type BadgeVariant, type ResultTone } from './ui';

export function WaResultPanel({ title, phone, result, loading }: { title: string; phone?: string; result?: WaWorkflowResponse | null; loading?: boolean }) {
  const status = waProbeStatus(result);
  const outcome = outcomeMeta(status, result, loading);
  return (
    <section className="grid gap-3 rounded-xl border border-border bg-card p-4">
      <div className="flex items-center justify-between gap-3">
        <div><h3 className="text-sm font-semibold">{title}</h3>{phone && <p className="text-xs text-muted-foreground">{phone}</p>}</div>
        <Badge variant={outcome.variant}>{outcome.label}</Badge>
      </div>
      <div className="grid gap-2 sm:grid-cols-3">{waMetrics(status).map((item) => <Metric key={item.label} {...item} />)}</div>
      {status.methodStatuses.length > 0 && <div className="flex flex-wrap gap-2">{status.methodStatuses.map((method) => <Badge key={method.key} variant="outline">{method.label}：{methodStateLabel(method.available, method.cooldownSeconds)}</Badge>)}</div>}
      <div className="grid gap-1 text-xs text-muted-foreground">{metaItems(status, result).map((item) => <span key={item.label} className={toneClass(item.tone)}>{item.label}：{item.value}</span>)}</div>
    </section>
  );
}

function Metric({ label, value, tone }: { label: string; value: string; tone: ResultTone }) {
  return <div className={`rounded-lg border border-border p-3 ${toneClass(tone)}`}><div className="text-xs text-muted-foreground">{label}</div><div className="text-sm font-semibold">{value}</div></div>;
}

function waMetrics(status: WaProbeStatus): Array<{ label: string; value: string; tone: ResultTone }> {
  if (status.requestFailed) {
    return [
      { label: '请求', value: '失败', tone: 'bad' },
      { label: 'raw_status', value: status.accountRawStatus || '-', tone: 'bad' },
      { label: 'raw_reason', value: status.accountRawReason || '-', tone: 'bad' },
    ];
  }
  return [
    { label: '旧设备', value: oldDeviceLabel(status.registered, status.accountFlow), tone: oldDeviceTone(status) },
    { label: 'SMS', value: smsLabel(status.smsAvailable, status.smsWaitSeconds), tone: smsTone(status) },
    { label: '封禁', value: booleanLabel(status.blocked), tone: booleanTone(status.blocked) },
  ];
}

function oldDeviceTone(status: WaProbeStatus): ResultTone {
  if (status.registered === true || status.accountFlow === 'registered') return 'warn';
  if (status.accountFlow === 'blocked') return 'bad';
  return 'idle';
}
function smsTone(status: WaProbeStatus): ResultTone {
  if (status.smsAvailable === true && !status.smsWaitSeconds) return 'ok';
  if (status.smsAvailable === false || Boolean(status.smsWaitSeconds)) return 'warn';
  return 'idle';
}
function booleanTone(value?: boolean): ResultTone {
  if (value === true) return 'bad';
  if (value === false) return 'ok';
  return 'idle';
}
function toneClass(tone?: ResultTone | BadgeVariant) {
  if (tone === 'ok') return 'text-emerald-700';
  if (tone === 'warn') return 'text-amber-700';
  if (tone === 'bad') return 'text-destructive';
  return '';
}
