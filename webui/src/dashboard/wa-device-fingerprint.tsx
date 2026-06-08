import { Cpu, Fingerprint, Loader2, Smartphone } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import type { ClientProfile, DeviceFingerprint } from '../proto/byte/v/forge/waapp/v1/profile';
import { Badge } from './ui';

export function WaDeviceFingerprintPanel({ profiles, loading }: { profiles: ClientProfile[]; loading: boolean }) {
  if (loading) return <p className="inline-flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />加载设备指纹...</p>;
  if (profiles.length === 0) return <p className="text-sm text-muted-foreground">暂无客户端 Profile。</p>;
  return <div className="grid gap-3">{profiles.map((profile) => <ProfileCard key={profile.client_profile_id} profile={profile} />)}</div>;
}

function ProfileCard({ profile }: { profile: ClientProfile }) {
  const fp = profile.device_fingerprint;
  return (
    <article className="grid gap-3 rounded-xl border border-border bg-muted/20 p-3">
      <header className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate font-mono text-xs text-muted-foreground">{profile.client_profile_id}</p>
          <h4 className="mt-1 text-sm font-semibold">{deviceTitle(fp)}</h4>
        </div>
        <Badge variant="outline">{profile.status || 'UNKNOWN'}</Badge>
      </header>
      {fp ? <FingerprintGrid fingerprint={fp} /> : <p className="text-sm text-muted-foreground">没有可展示的设备指纹。</p>}
    </article>
  );
}

function FingerprintGrid({ fingerprint }: { fingerprint: DeviceFingerprint }) {
  const rows: Array<{ label: string; value: string; icon: LucideIcon }> = [
    { label: '指纹 ID', value: fingerprint.fingerprint_id, icon: Fingerprint },
    { label: 'FDID', value: fingerprint.fdid, icon: Fingerprint },
    { label: 'User-Agent', value: fingerprint.user_agent, icon: Smartphone },
    { label: 'App / Android', value: [fingerprint.app_version, fingerprint.android_version].filter(Boolean).join(' / '), icon: Smartphone },
    { label: 'RAM / Radio', value: [ramLabel(fingerprint.device_ram_gib), radioLabel(fingerprint.network_radio_type)].filter(Boolean).join(' / '), icon: Cpu },
    { label: 'MCC/MNC', value: pairLabel(fingerprint.mcc, fingerprint.mnc), icon: Smartphone },
    { label: 'SIM MCC/MNC', value: pairLabel(fingerprint.sim_mcc, fingerprint.sim_mnc), icon: Smartphone },
    { label: 'Phone Hash', value: fingerprint.phone_sha256_prefix ? `${fingerprint.phone_sha256_prefix}...` : '', icon: Fingerprint },
    { label: '生成时间', value: formatTime(fingerprint.created_at), icon: Smartphone },
  ];
  return (
    <dl className="grid gap-2 md:grid-cols-2">
      {rows.map(({ label, value, icon: Icon }) => (
        <div className="min-w-0 rounded-lg bg-card p-3" key={label}>
          <dt className="inline-flex items-center gap-1.5 text-xs text-muted-foreground"><Icon size={13} />{label}</dt>
          <dd className="mt-1 truncate font-mono text-xs">{value || '-'}</dd>
        </div>
      ))}
    </dl>
  );
}

function deviceTitle(fingerprint?: DeviceFingerprint) {
  if (!fingerprint) return '未知设备';
  return [fingerprint.device_vendor, fingerprint.device_model].filter(Boolean).join(' ') || '未知设备';
}

function pairLabel(a?: string, b?: string) {
  return [a, b].filter(Boolean).join('/');
}

function ramLabel(value?: string) {
  return value ? `${value} GiB` : '';
}

function radioLabel(value?: string) {
  const labels: Record<string, string> = { '1': 'GPRS', '2': 'EDGE', '3': 'UMTS', '9': 'HSDPA', '13': 'LTE', '20': 'NR' };
  return value ? labels[value] || value : '';
}

function formatTime(value?: string) {
  if (!value) return '';
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? value : time.toLocaleString();
}
