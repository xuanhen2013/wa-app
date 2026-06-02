import { Copy, Play, Search, Smartphone } from 'lucide-react';
import {
  AccountActionRow,
  AccountActionRows,
  AccountCarrierManualOTPSubmit,
  AccountDangerZone,
  AccountDetails,
  Badge,
  Button,
  accountId,
  accountStatusValue,
  accountSubjectRenderConfig,
  buttonHint,
  copyText,
  useQuery,
  type AccountManagementDetailTab,
  type AccountRecord,
  type ActionButtonDescriptor,
} from '@byte-v-forge/common-ui';
import { WaOtpSource } from '@byte-v-forge/common-ui/proto/byte/v/forge/contracts/wa/v1/wa';
import type { OtpMessage } from '../proto/byte/v/forge/waapp/v1/extraction';
import type { WaAccountProjection, WaWorkflowResponse } from './wa-api';
import { getWaAccountOtpMessages, submitWaRegistrationOTP, waKeys } from './wa-api';
import { WaResultPanel } from './wa-result-panel';

const ACCOUNT_WORKSPACE_ID = 'default';

export type WaAccountActionResult = {
  accountID: string;
  kind: 'register' | 'probe';
  result: WaWorkflowResponse | null;
  phone?: string;
};

export function waAccountDetailTabs(options: {
  actionResult: WaAccountActionResult | null;
  busy: boolean;
  onRegister: (account: WaAccountProjection) => void | Promise<void>;
  onProbe: (account: WaAccountProjection) => void | Promise<void>;
  onDelete: (account: WaAccountProjection) => void | Promise<void>;
  onManualOTPDone: (message: string) => void;
  onError: (message: string) => void;
}) {
  return (carrier: WaAccountProjection, account: AccountRecord): AccountManagementDetailTab[] => [
    {
      value: 'details',
      label: '账户详情',
      content: <WaAccountOverview carrier={carrier} account={account} actionResult={options.actionResult} busy={options.busy} onRegister={options.onRegister} onProbe={options.onProbe} onDelete={options.onDelete} onManualOTPDone={options.onManualOTPDone} onError={options.onError} />,
    },
    { value: 'otp', label: 'OTP 历史', content: <WaOtpHistory account={account} /> },
  ];
}

function WaAccountOverview({ carrier, account, actionResult, busy, onRegister, onProbe, onDelete, onManualOTPDone, onError }: {
  carrier: WaAccountProjection;
  account: AccountRecord;
  actionResult: WaAccountActionResult | null;
  busy: boolean;
  onRegister: (account: WaAccountProjection) => void | Promise<void>;
  onProbe: (account: WaAccountProjection) => void | Promise<void>;
  onDelete: (account: WaAccountProjection) => void | Promise<void>;
  onManualOTPDone: (message: string) => void;
  onError: (message: string) => void;
}) {
  const currentResult = actionResult?.accountID === accountId(account) ? actionResult : null;
  return (
    <div className="grid gap-0">
      <div className="grid gap-3 p-4 pb-0">
        <WaAccountActions account={account} busy={busy} onRegister={() => onRegister(carrier)} onProbe={() => onProbe(carrier)} />
        <AccountCarrierManualOTPSubmit
          account={carrier}
          keyPrefix="wa-manual-otp"
          subtitle="只把本次输入提交给当前等待中的注册流程，不写入 OTP 历史。"
          disabled={busy}
          submit={submitWaRegistrationOTP}
          onSuccess={(resp) => {
            if (resp.error_message || resp.success === false) throw new Error(resp.error_message || 'OTP 提交失败');
            onManualOTPDone('OTP 已提交到等待中的注册流程');
          }}
          onError={(error) => onError(error instanceof Error ? error.message : String(error))}
        />
        {currentResult && <WaResultPanel title={currentResult.kind === 'register' ? '注册结果' : '探测结果'} phone={currentResult.phone} result={currentResult.result} loading={busy} />}
      </div>
      <AccountDetails
        account={account}
        config={accountSubjectRenderConfig({
          icon: () => <Smartphone size={15} />,
          showSubtitle: false,
          showStatusMeta: false,
        })}
      />
      <div className="px-4 pb-4">
        <AccountDangerZone account={carrier} busy={busy} onDelete={onDelete} />
      </div>
    </div>
  );
}

function WaAccountActions({ account, busy, onRegister, onProbe }: {
  account: AccountRecord;
  busy: boolean;
  onRegister: () => void | Promise<void>;
  onProbe: () => void | Promise<void>;
}) {
  return (
    <AccountActionRows>
      <AccountActionRow label="注册" actions={registerActions(account, busy, onRegister)} />
      <AccountActionRow label="工具" actions={toolActions(busy, onProbe)} />
    </AccountActionRows>
  );
}

function registerActions(account: AccountRecord, busy: boolean, onRegister: () => void | Promise<void>): ActionButtonDescriptor[] {
  const status = accountStatusValue(account);
  const registered = status === 'active';
  return [{
    id: 'wa-register',
    label: '注册流程',
    icon: <Play size={14} />,
    disabled: busy || registered || status === 'archived',
    hint: registered ? '当前 WAAccount 已激活' : '进入 WA 注册流程并等待 OTP',
    onClick: () => { void onRegister(); },
  }];
}

function toolActions(busy: boolean, onProbe: () => void | Promise<void>): ActionButtonDescriptor[] {
  return [{
    id: 'wa-probe',
    label: '手机号/SMS 探测',
    icon: <Search size={14} />,
    disabled: busy,
    variant: 'outline',
    hint: '对当前 WAAccount 的手机号重新探测旧设备和 SMS 状态',
    onClick: () => { void onProbe(); },
  }];
}

function WaOtpHistory({ account }: { account: AccountRecord }) {
  const waAccountId = accountId(account);
  const query = useQuery({ queryKey: waKeys.otpMessages(ACCOUNT_WORKSPACE_ID, waAccountId), queryFn: () => getWaAccountOtpMessages(ACCOUNT_WORKSPACE_ID, waAccountId), enabled: Boolean(waAccountId), refetchInterval: 10000 });
  const messages = query.data?.otp_messages || [];
  return <section className="grid gap-2"><div className="flex items-center justify-between"><h3 className="text-sm font-semibold text-foreground">OTP 历史</h3><Badge variant="outline">{messages.length} 条</Badge></div>{query.isLoading ? <div className="rounded-xl border bg-card p-3 text-sm text-muted-foreground">加载 OTP 历史...</div> : messages.length === 0 ? <div className="rounded-xl border bg-card p-3 text-sm text-muted-foreground">暂无 OTP 历史</div> : <div className="grid gap-2">{messages.map((item) => <WaOtpHistoryRow key={item.otp_message_id} item={item} />)}</div>}{query.data?.error?.message && <p className="text-xs text-destructive">{query.data.error.message}</p>}</section>;
}

function WaOtpHistoryRow({ item }: { item: OtpMessage }) {
  const sender = sourcePartyLabel(item.source_party);
  return <div className="grid gap-1 rounded-xl border bg-card p-3 text-sm"><div className="flex items-center justify-between gap-3"><span className="font-mono text-base">{item.otp?.value || item.otp?.redacted_value || '-'}</span><Badge variant="outline">{otpSourceLabel(item.source)}</Badge></div><div className="grid gap-1 text-xs text-muted-foreground"><div className="flex items-center gap-1"><span>发送方：{sender.label}</span>{sender.detail && <code className="rounded bg-muted px-1 font-mono">{sender.detail}</code>}{item.source_party && <Button className="h-5 px-1" variant="ghost" {...buttonHint('复制发送方标识')} onClick={() => { void copyText(item.source_party); }}><Copy size={12} /></Button>}</div><span>接收时间：{formatTime(item.received_at)}</span></div></div>;
}

function sourcePartyLabel(value?: string) {
  const raw = (value || '').trim();
  if (!raw) return { label: '-', detail: '' };
  if (raw === 's.whatsapp.net') return { label: 'WhatsApp 系统', detail: '' };
  if (raw.endsWith('@lid')) return { label: 'WA LID（未解析联系人）', detail: raw.replace(/@lid$/, '') };
  if (raw.endsWith('@g.us')) return { label: 'WA 群组', detail: raw };
  if (raw.endsWith('@s.whatsapp.net')) return { label: 'WA 号码', detail: `+${raw.replace(/@s\.whatsapp\.net$/, '')}` };
  return { label: 'WA 标识', detail: raw };
}

function otpSourceLabel(source: WaOtpSource | undefined) {
  switch (source) {
    case WaOtpSource.WA_OTP_SOURCE_LONG_CONNECTION: return '长连接';
    case WaOtpSource.WA_OTP_SOURCE_IMPORTED_HISTORY: return '导入历史';
    case WaOtpSource.WA_OTP_SOURCE_AUTO_EXTRACTION: return '自动解析';
    default: return '未知';
  }
}

function formatTime(value?: string) {
  if (!value) return '-';
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? value : time.toLocaleString();
}
