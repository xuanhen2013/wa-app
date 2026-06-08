import { LongConnectionStatus, type LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';

export function WaConnectionDot({ connection, loading = false, className = '' }: { connection?: LongConnectionState; loading?: boolean; className?: string }) {
  const view = connectionView(connection?.status, loading);
  return <span className={`inline-block size-2.5 rounded-full ${view.className} ${className}`} title={view.label} aria-label={view.label} />;
}

export function connectionView(status: LongConnectionStatus | undefined, loading: boolean) {
  if (loading && !status) return { label: '连接状态加载中', className: 'bg-slate-300' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_CONNECTED || status === LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING) return { label: '已连接', className: 'bg-emerald-500' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_RECONNECTING || status === LongConnectionStatus.LONG_CONNECTION_STATUS_STARTING) return { label: '连接中', className: 'bg-amber-500' };
  if (status === LongConnectionStatus.LONG_CONNECTION_STATUS_FAILED) return { label: '连接失败', className: 'bg-destructive' };
  return { label: '未连接', className: 'bg-slate-300' };
}
