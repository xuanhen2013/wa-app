import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Ban, CheckCircle2, Loader2, RefreshCw, WandSparkles } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { DEFAULT_WA_INTEGRITY_MODE, type WaIntegrityMode } from './wa-integrity';
import { WaIntegrityModeSelect } from './wa-integrity-mode-select';
import { waPlayIntegrityAvailable } from './wa-dashboard-config';
import { useWaDashboardHealth, useWaPlayIntegrityAPIStatus } from './wa-dashboard-hooks';
import { cancelBulkRegistrationTask, createBulkRegistrationTask, getBulkRegistrationCountries, getBulkRegistrationOffers, getBulkRegistrationTask, type BulkRegistrationItem, type BulkRegistrationOffer, type BulkRegistrationTask, waKeys } from './wa-api';

type Props = { disabled?: boolean; onChanged: () => void | Promise<void>; onDone: (message: string) => void; onError: (message: string) => void };

export function WaBulkAccountAdd({ disabled, onChanged, onDone, onError }: Props) {
  const queryClient = useQueryClient();
  const [countryISO2, setCountryISO2] = useState('');
  const [targetCount, setTargetCount] = useState(10);
  const [integrityMode, setIntegrityMode] = useState<WaIntegrityMode>(DEFAULT_WA_INTEGRITY_MODE);
  const [quantities, setQuantities] = useState<Record<string, number>>({});
  const health = useWaDashboardHealth();
  const playIntegrityAvailable = waPlayIntegrityAvailable(health);
  const { status: integrityStatus, loading: integrityStatusLoading } = useWaPlayIntegrityAPIStatus(playIntegrityAvailable, integrityMode);
  const taskQuery = useQuery({ queryKey: waKeys.bulkRegistrationTask(), queryFn: getBulkRegistrationTask, refetchInterval: (query) => query.state.data?.task && !taskFinished(query.state.data.task) ? 2000 : false });
  const countriesQuery = useQuery({ queryKey: waKeys.bulkRegistrationCountries(), queryFn: getBulkRegistrationCountries, staleTime: 5 * 60 * 1000 });
  const offersQuery = useQuery({ queryKey: ['wa', 'bulk-registration', 'offers', countryISO2], queryFn: () => getBulkRegistrationOffers(countryISO2), enabled: false });
  const { data: offersData, isError: offersFailed, isFetching: offersFetching, refetch: refetchOffers } = offersQuery;
  const task = taskQuery.data?.task;
  const countries = useMemo(() => countriesQuery.data?.countries || [], [countriesQuery.data?.countries]);
  const offers = offersData?.offers || [];
  const selectedCount = useMemo(() => Object.values(quantities).reduce((sum, quantity) => sum + Math.max(0, quantity || 0), 0), [quantities]);
  const createTask = useMutation({
    mutationFn: () => createBulkRegistrationTask({
      country_iso2: countryISO2,
      target_count: targetCount,
      integrity_mode: playIntegrityAvailable ? integrityMode : DEFAULT_WA_INTEGRITY_MODE,
      offers: offers.filter((offer) => (quantities[offer.offer_id] || 0) > 0).map((offer) => ({ offer_id: offer.offer_id, quantity: quantities[offer.offer_id], max_price: offer.price })),
    }),
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey: waKeys.bulkRegistrationTask() });
      if (!response.existing) onDone('批量任务已提交');
      await onChanged();
    },
    onError: (error) => onError(error instanceof Error ? error.message : String(error)),
  });
  const cancelTask = useMutation({
    mutationFn: cancelBulkRegistrationTask,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: waKeys.bulkRegistrationTask() });
      onDone('已请求取消批量任务');
    },
    onError: (error) => onError(error instanceof Error ? error.message : String(error)),
  });

  useEffect(() => {
    if (task || !countryISO2 || offersData || offersFetching || offersFailed) return;
    void refetchOffers();
  }, [countryISO2, offersData, offersFailed, offersFetching, refetchOffers, task]);

  useEffect(() => {
    if (countries.length === 0 || countries.some((country) => country.country_iso2 === countryISO2)) return;
    setCountryISO2(countries[0].country_iso2);
    setQuantities({});
  }, [countries, countryISO2]);

  if (task) return <BulkTaskDetail task={task} items={taskQuery.data?.items || []} canceling={cancelTask.isPending} onCancel={() => void cancelTask.mutateAsync()} />;

  function setQuantity(offer: BulkRegistrationOffer, nextValue: number) {
    const bounded = Math.max(0, Math.min(offer.available_count, Number.isFinite(nextValue) ? Math.floor(nextValue) : 0));
    setQuantities((current) => ({ ...current, [offer.offer_id]: bounded }));
  }
  function selectCountry(nextCountryISO2: string) {
    setCountryISO2(nextCountryISO2);
    setQuantities({});
  }
  function autoSelectLowestPrice() {
    let remaining = targetCount;
    const next: Record<string, number> = {};
    for (const offer of offers) {
      const quantity = Math.min(remaining, offer.available_count);
      if (quantity > 0) next[offer.offer_id] = quantity;
      remaining -= quantity;
      if (remaining <= 0) break;
    }
    setQuantities(next);
    if (remaining > 0) onError('当前报价库存不足以满足目标数量');
  }
  const busy = Boolean(disabled || countriesQuery.isLoading || offersQuery.isFetching || createTask.isPending);
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-3">
        <div className="grid gap-1"><CardTitle className="text-base">批量添加账号</CardTitle></div>
        <Badge variant={offersData ? 'default' : 'outline'}>{offersFetching ? <Loader2 className="size-3 animate-spin" /> : null}{offersData ? '报价已加载' : '待加载报价'}</Badge>
      </CardHeader>
      <CardContent className="grid gap-4">
        <FieldGroup>
          <div className="grid gap-3 sm:grid-cols-[1fr_1fr_auto]">
            <Field><FieldLabel>地区</FieldLabel><Select value={countryISO2} onValueChange={selectCountry} disabled={busy || countries.length === 0}><SelectTrigger className="w-full"><SelectValue placeholder={countriesQuery.isLoading ? '加载地区...' : '选择地区'} /></SelectTrigger><SelectContent>{countries.map((country) => <SelectItem key={country.country_iso2} value={country.country_iso2}>{country.name} ({country.country_iso2})</SelectItem>)}</SelectContent></Select></Field>
            <Field><FieldLabel>目标数量</FieldLabel><Input type="number" min={1} max={offersData?.max_items || 10} value={targetCount} onChange={(event) => setTargetCount(Math.max(1, Number(event.target.value) || 1))} disabled={busy} /></Field>
            <Field className="justify-end"><FieldLabel className="sr-only">刷新报价</FieldLabel><Button type="button" size="icon" variant="outline" title="刷新报价" aria-label="刷新报价" disabled={busy} onClick={() => void refetchOffers()}><RefreshCw className={offersFetching ? 'size-4 animate-spin' : 'size-4'} /></Button></Field>
          </div>
        </FieldGroup>
        <WaIntegrityModeSelect available={playIntegrityAvailable} disabled={busy} status={integrityStatus} statusLoading={integrityStatusLoading} value={integrityMode} onChange={setIntegrityMode} />
        {countriesQuery.error ? <p className="text-sm text-destructive">{countriesQuery.error instanceof Error ? countriesQuery.error.message : '加载地区失败'}</p> : null}
        {countriesQuery.data && countries.length === 0 ? <p className="text-sm text-muted-foreground">当前暂无可用地区</p> : null}
        {offersQuery.error ? <p className="text-sm text-destructive">{offersQuery.error instanceof Error ? offersQuery.error.message : '加载报价失败'}</p> : null}
        <div className="flex items-center justify-between gap-3 border-b pb-3">
          <span className="text-sm text-muted-foreground">已选择 {selectedCount} / {targetCount}</span>
          <Button type="button" variant="outline" size="sm" disabled={busy || offers.length === 0} onClick={autoSelectLowestPrice}><WandSparkles className="size-4" />自动选择最低价</Button>
        </div>
        <OfferTable offers={offers} quantities={quantities} busy={busy} onQuantityChange={setQuantity} />
        <Button type="button" disabled={busy || selectedCount !== targetCount || offers.length === 0} onClick={() => void createTask.mutateAsync()}>{createTask.isPending ? <Loader2 className="size-4 animate-spin" /> : <CheckCircle2 className="size-4" />}提交任务</Button>
      </CardContent>
    </Card>
  );
}

function OfferTable({ offers, quantities, busy, onQuantityChange }: { offers: BulkRegistrationOffer[]; quantities: Record<string, number>; busy: boolean; onQuantityChange: (offer: BulkRegistrationOffer, quantity: number) => void }) {
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader><TableRow><TableHead>供应商</TableHead><TableHead>地区</TableHead><TableHead>价格</TableHead><TableHead>库存</TableHead><TableHead>选择数量</TableHead></TableRow></TableHeader>
        <TableBody>
          {offers.map((offer) => <TableRow key={offer.offer_id}><TableCell>{offer.provider}</TableCell><TableCell>{offer.country_iso2}</TableCell><TableCell>{formatMoney(offer.price, offer.currency)}</TableCell><TableCell>{offer.available_count.toLocaleString()}</TableCell><TableCell><Input className="h-8 w-24" type="number" min={0} max={offer.available_count} value={quantities[offer.offer_id] || 0} disabled={busy} onChange={(event) => onQuantityChange(offer, Number(event.target.value))} /></TableCell></TableRow>)}
          {offers.length === 0 ? <TableRow><TableCell colSpan={5} className="h-24 text-center text-muted-foreground">暂无可用报价</TableCell></TableRow> : null}
        </TableBody>
      </Table>
    </div>
  );
}

function BulkTaskDetail({ task, items, canceling, onCancel }: { task: BulkRegistrationTask; items: BulkRegistrationItem[]; canceling: boolean; onCancel: () => void }) {
  const cancelable = !taskFinished(task) && task.status !== 'CANCEL_REQUESTED' && task.status !== 'CANCELING';
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-3">
        <div className="grid gap-1"><CardTitle className="text-base">批量注册任务</CardTitle><span className="font-mono text-xs text-muted-foreground">{task.task_id}</span></div>
        <div className="flex items-center gap-2"><Badge variant={task.status === 'RUNNING' ? 'default' : 'secondary'} title={task.status}>{taskStatusLabel(task.status)}</Badge>{cancelable ? <Button type="button" variant="destructive" size="sm" disabled={canceling} onClick={onCancel}>{canceling ? <Loader2 className="size-4 animate-spin" /> : <Ban className="size-4" />}取消任务</Button> : null}</div>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm sm:grid-cols-5"><TaskMetric label="目标" value={task.target_count} /><TaskMetric label="成功" value={task.success_count} /><TaskMetric label="失败" value={task.failed_count} /><TaskMetric label="取消" value={task.canceled_count} /><TaskMetric label="处理中" value={task.waiting_count} /></div>
        {task.last_error ? <p className="text-sm text-destructive"><span className="font-medium">最近错误：</span>{formatBulkFailure(task.last_error)}</p> : null}
        <div className="overflow-x-auto">
          <Table>
            <TableHeader><TableRow><TableHead>#</TableHead><TableHead>供应商</TableHead><TableHead>号码</TableHead><TableHead>阶段</TableHead><TableHead>短信</TableHead><TableHead>WA</TableHead><TableHead>根因</TableHead></TableRow></TableHeader>
            <TableBody>{items.map((item, index) => <TableRow key={item.item_id}><TableCell>{index + 1}</TableCell><TableCell>{item.provider}</TableCell><TableCell className="font-mono">{item.phone_masked || '-'}</TableCell><TableCell><StatusValue value={item.status} label={itemStatusLabel(item.status, item.cancel_attempt_count)} /></TableCell><TableCell><StatusValue value={item.sms_status} label={smsStatusLabel(item.sms_status)} /></TableCell><TableCell><StatusValue value={waStatus(item)} label={waStatusLabel(waStatus(item))} /></TableCell><TableCell className="max-w-64" title={item.last_error}><span className="line-clamp-2">{formatBulkFailure(item.last_error) || '-'}</span></TableCell></TableRow>)}</TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  );
}

function TaskMetric({ label, value }: { label: string; value: number }) {
  return <div className="grid gap-0.5"><span className="text-xs text-muted-foreground">{label}</span><strong className="font-mono text-lg">{value}</strong></div>;
}

function StatusValue({ value, label }: { value?: string; label: string }) {
  return <div className="grid gap-0.5"><span>{label}</span>{value ? <span className="font-mono text-xs text-muted-foreground" title={value}>{value}</span> : null}</div>;
}

function taskStatusLabel(value: string) {
  return ({ DRAFT: '草稿', RUNNING: '执行中', CANCEL_REQUESTED: '已请求取消', CANCELING: '取消中', COMPLETED: '已完成', PARTIAL_COMPLETED: '部分完成', FAILED: '失败', CANCELED: '已取消', PAUSED: '已暂停' } as Record<string, string>)[value] || '未知状态';
}

function itemStatusLabel(value: string, cancelAttempts: number) {
  const labels: Record<string, string> = { QUEUED: '排队中', ACQUIRING_NUMBER: '申请号码中', NUMBER_ACQUIRED: '号码已获取', WA_PROBING: 'WA 注册中', WA_REQUESTING_OTP: '请求验证码中', WAITING_SMS: '等待短信', SMS_RECEIVED: '收到验证码', SUBMITTING_OTP: '提交验证码中', REGISTERED: '注册成功', CANCELING_NUMBER: '正在取消短信号码', NUMBER_CANCELED: '短信号码已取消', FAILED: '失败', CANCELED: '已取消' };
  if (value === 'CANCEL_PENDING') return cancelAttempts > 0 ? `短信取消待确认（已尝试 ${cancelAttempts} 次）` : '短信取消待确认';
  return labels[value] || '未知阶段';
}

function smsStatusLabel(value: string) {
  return ({ QUEUED: '等待申请', NUMBER_ACQUIRED: '号码已获取', STATUS_WAIT_CODE: '等待验证码', STATUS_OK: '已收到验证码', STATUS_CANCEL: '已取消', STATUS_WAIT_RETRY: '等待重试', STATUS_WAIT_RESEND: '等待重发' } as Record<string, string>)[value] || (value ? '供应商状态' : '未开始');
}

function waStatus(item: BulkRegistrationItem) {
  return item.wa_registration_status || item.wa_verification_status || item.wa_probe_status || '';
}

function waStatusLabel(value: string) {
  return ({ RUNNING: '进行中', REQUESTED: '已请求', SENT: '已发送', WAITING: '等待验证', SUBMITTED: '已提交', REGISTERED: '已注册', REJECTED: '已拒绝', EXPIRED: '已过期' } as Record<string, string>)[value] || (value ? 'WA 状态' : '未开始');
}

function formatBulkFailure(value?: string) {
  const unique = [...new Set((value || '').split(';').map((part) => part.trim()).filter(Boolean))];
  return unique.map((part) => {
    if (part.startsWith('verification request was rejected')) {
      const reason = part.match(/reason=([^\s;]+)/)?.[1];
      return reason ? `WA 拒绝请求验证码（${reason}）` : 'WA 拒绝请求验证码';
    }
    if (part.startsWith('SMS activation cancellation pending') || part === 'SMS activation cancellation is pending') {
      const detail = part.split(':').slice(1).join(':').trim();
      return detail ? `短信平台取消待确认（${detail}）` : '短信平台取消待确认';
    }
    return part;
  }).join('；');
}

function taskFinished(task: BulkRegistrationTask) {
  return ['COMPLETED', 'PARTIAL_COMPLETED', 'FAILED', 'CANCELED'].includes(task.status);
}

function formatMoney(value: number, currency: string) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: currency || 'USD', minimumFractionDigits: 2 }).format(value);
}
