import { useState } from 'react';
import { RefreshCw, Smartphone, Workflow } from 'lucide-react';
import {
  ACCOUNT_PAGE_SIZE,
  AccountManagementDrawerView,
  AccountPhoneProbeToolbox,
  ToastMessage,
  WorkflowStatusPanel,
  WorkspaceTabbedPanel,
  accountCarrierID,
  accountId,
  accountSubjectRenderConfig,
  useAccountActionRunner,
  useAccountManagementController,
  useAccountProbeAction,
  useQuery,
  useToastMessage,
  type AccountManagementController,
  type AccountManagementControllerOptions,
  type AccountRecord,
} from '@byte-v-forge/common-ui';
import type { ListWAAccountsResponse } from '../proto/byte/v/forge/waapp/v1/profile';
import {
  deleteWaAccount,
  getWaAccounts,
  getWaHealth,
  probeWaAccount,
  probeWaPhoneSMS,
  registerWaAccount,
  waKeys,
  type WaAccountProjection,
  type WaWorkflowResponse,
} from './wa-api';
import { WaAccountAdd } from './wa-account-add';
import { waAccountDetailTabs, type WaAccountActionResult } from './wa-account-detail';
import { WaLongConnectionBadge, useWaLongConnectionIndex } from './wa-long-connection-badge';
import { WaResultPanel } from './wa-result-panel';
import { resolveWaPhoneTarget, type WaResolvedPhone } from './wa-utils';

type WaTab = 'accounts' | 'toolbox' | 'workflows';

const ACCOUNT_WORKSPACE_ID = 'default';
const waAccountControllerOptions = {
  queryKey: waKeys.accounts(ACCOUNT_WORKSPACE_ID),
  queryFn: (cursor) => getWaAccounts(ACCOUNT_WORKSPACE_ID, cursor),
  refetchInterval: 10000,
  pageSize: ACCOUNT_PAGE_SIZE,
  clearMissingSelection: true,
} satisfies AccountManagementControllerOptions<WaAccountProjection, ListWAAccountsResponse>;

export function WaPage() {
  const toast = useToastMessage();
  const health = useQuery({ queryKey: waKeys.health, queryFn: getWaHealth });
  const accounts = useAccountManagementController<WaAccountProjection, ListWAAccountsResponse>(waAccountControllerOptions);
  const phoneProbe = useAccountProbeAction<WaResolvedPhone, WaWorkflowResponse>({
    actionKey: 'wa-phone-sms-probe',
    subjectOf: (target) => target.e164,
    probe: (target) => probeWaPhoneSMS(target.input),
    onSuccess: () => toast.showOK('手机号/SMS 探测完成'),
    onError: toast.showError,
  });

  return <><ToastMessage toast={toast.toast} /><WorkspaceTabbedPanel<WaTab> defaultValue="accounts" title={<span className="inline-flex items-center gap-2"><Smartphone className="size-4" />WA 管理</span>} meta={`${accounts.accounts.length} 个账号 · ${health.data?.n8n_webhook_configured ? 'n8n 已接入' : '等待 n8n'}`} tabs={[
    { value: 'accounts', label: '账号', content: <WaAccountsTab controller={accounts} onAccountAdded={async () => { toast.showOK('WAAccount 已添加'); await accounts.invalidate(); }} onActionDone={toast.showOK} onError={toast.showError} /> },
    { value: 'toolbox', label: '工具箱', content: <ToolboxTab result={phoneProbe.result} phone={phoneProbe.subject} busy={phoneProbe.busy} onCheck={phoneProbe.run} onError={toast.showError} /> },
    { value: 'workflows', label: '工作流', content: <WorkflowTab configured={Boolean(health.data?.n8n_webhook_configured)} workflows={health.data?.workflows || []} loading={health.isLoading} /> }
  ]} /></>;
}

function ToolboxTab(props: { result: WaWorkflowResponse | null; phone: string; busy: boolean; onCheck: (target: WaResolvedPhone) => void | Promise<void>; onError: (message: unknown) => void }) {
  return (
    <AccountPhoneProbeToolbox<WaResolvedPhone, WaWorkflowResponse>
      title="手机号/SMS 探测"
      subject={props.phone}
      result={props.result}
      busy={props.busy}
      emptyResultText="结果：旧设备 / SMS / Blocked"
      countryPlaceholder="+992"
      phonePlaceholder="007886231"
      actionLabel="探测手机号和 SMS 状态"
      resolve={(values) => resolveWaPhoneTarget(values.phone, values.country_calling_code)}
      renderResult={({ subject, result, loading }) => <WaResultPanel title="探测结果" phone={subject} result={result} loading={loading} />}
      onSubmit={props.onCheck}
      onError={props.onError}
    />
  );
}

function WaAccountsTab(props: { controller: AccountManagementController<WaAccountProjection, ListWAAccountsResponse>; onAccountAdded: () => void | Promise<void>; onActionDone: (message: string) => void; onError: (message: unknown) => void }) {
  const [actionResult, setActionResult] = useState<WaAccountActionResult | null>(null);
  const runner = useAccountActionRunner();
  const busy = props.controller.isLoading || props.controller.actionBusy || runner.busy;
  const connections = useWaLongConnectionIndex(ACCOUNT_WORKSPACE_ID);
  const renderConfig = {
    ...accountSubjectRenderConfig({ icon: () => <Smartphone size={15} /> }),
    meta: (account: AccountRecord) => <WaLongConnectionBadge connection={connections.byAccount.get(accountId(account))} loading={connections.loading} />,
  };
  async function deleteAccount(account: WaAccountProjection) {
    const accountID = accountCarrierID(account);
    await props.controller.deleteAccount(account, () => deleteWaAccount(account, ACCOUNT_WORKSPACE_ID), {
      actionID: 'wa-delete',
      confirmMessage: () => `删除 WAAccount ${accountID}？`,
      onSuccess: (deleted) => {
        if (deleted) props.onActionDone('WAAccount 已删除');
      },
      onError: props.onError,
    });
  }
  return <AccountManagementDrawerView title="WAAccount" icon={<Smartphone size={16} />} actions={<WaAccountAdd disabled={busy} onCreated={props.onAccountAdded} onError={props.onError} />} carriers={props.controller.accounts} selectedCarrier={props.controller.selected} selectedID={props.controller.selectedID} onSelectCarrier={props.controller.selectAccount} loading={props.controller.isLoading} loadingText="加载 WAAccount..." emptyText="暂无已持久化 WAAccount" pagination={props.controller.accountsPagination} config={renderConfig} drawerDescription="WA 账号详情" detailTabs={waAccountDetailTabs({ actionResult, busy, onRegister: (account) => runWAAccountAction('register', account, runner, setActionResult, { ...props, onAccountsChanged: props.controller.invalidate }), onProbe: (account) => runWAAccountAction('probe', account, runner, setActionResult, { ...props, onAccountsChanged: props.controller.invalidate }), onDelete: deleteAccount, onManualOTPDone: props.onActionDone, onError: props.onError })} onCloseDetails={props.controller.clearSelection} />;
}

type WaAccountActionKind = WaAccountActionResult['kind'];
type WaAccountRunner = ReturnType<typeof useAccountActionRunner>;
type WaAccountActionCallbacks = {
  onAccountsChanged: () => void | Promise<void>;
  onActionDone: (message: string) => void;
  onError: (message: unknown) => void;
};

async function runWAAccountAction(kind: WaAccountActionKind, account: WaAccountProjection, runner: WaAccountRunner, setActionResult: (value: WaAccountActionResult | null) => void, callbacks: WaAccountActionCallbacks) {
  const accountID = accountCarrierID(account);
  const phone = account.phone?.e164_number || '';
  await runner.tryRunAccountAction(`wa-${kind}`, account, async () => {
    const result = kind === 'register' ? await registerWaAccount(account) : await probeWaAccount(account);
    setActionResult({ accountID, kind, phone, result });
    const error = waAccountActionError(kind, result);
    if (error) throw new Error(error);
    callbacks.onActionDone(kind === 'register' ? 'WA 注册流程已触发' : '手机号/SMS 探测完成');
    if (kind === 'register') await callbacks.onAccountsChanged();
  }, { onError: callbacks.onError });
}

function waAccountActionError(kind: WaAccountActionKind, result: WaWorkflowResponse) {
  const errorText = textOf(result.error_message) || textOf((result as Record<string, unknown>).error);
  if (result.request_failed) return errorText || 'WA 请求失败';
  if (kind === 'register' && result.success === false) return errorText || textOf(result.status) || 'WA 注册流程失败';
  return '';
}

function textOf(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function WorkflowTab({ configured, workflows, loading }: { configured: boolean; loading?: boolean; workflows: Array<{ key: string; label: string; webhook_path: string }> }) {
  return (
    <WorkflowStatusPanel
      configured={configured}
      loading={loading}
      configuredTitle="WA n8n 编排已接入"
      unconfiguredTitle="WA n8n webhook 未配置"
      description="注册流程走 workflow；工具箱号码/SMS 探测、登录态检测、长连接恢复和 OTP MQ 投放由 wa-app 直连服务完成。"
      cards={[
        {
          id: 'register',
          icon: <Workflow size={16} />,
          title: '注册流程',
          badge: 'n8n',
          text: '跨步骤注册和等待 OTP 仍由 n8n 编排。',
        },
        {
          id: 'direct',
          icon: <RefreshCw size={16} />,
          title: '探测 / 登录态 / 长连接',
          badge: '直连',
          text: '号码/SMS 探测使用 1 分钟动态 IP 短租约，用完释放；登录态和长连接不进入 workflow。',
        },
      ]}
      workflows={workflows}
    />
  );
}
