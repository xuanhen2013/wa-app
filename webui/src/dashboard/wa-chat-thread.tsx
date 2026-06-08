import { AssistantRuntimeProvider, MessagePrimitive, ThreadPrimitive, useExternalStoreRuntime, useMessage, type AppendMessage } from '@assistant-ui/react';
import { Copy, Info, Loader2 } from 'lucide-react';
import { Link } from 'react-router';
import type { LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { waAccountID, waAccountTitle } from './wa-api';
import { WhatsAppIcon } from './wa-brand-icon';
import { toAssistantMessage, type WaChatEvent, type WaChatMeta, type WaContact } from './wa-chat-model';
import { WaConnectionDot } from './wa-connection-dot';
import { WaMessageContent } from './wa-message-content';
import { waAccountPath } from './wa-route-paths';
import { Badge } from './ui';

export function WaChatThread({ account, connection, contact, events, loading, error }: { account: WAAccount; connection?: LongConnectionState; contact?: WaContact; events: WaChatEvent[]; loading: boolean; error?: string }) {
  const runtime = useExternalStoreRuntime<WaChatEvent>({ messages: events, convertMessage: toAssistantMessage, isDisabled: true, isLoading: loading, onNew: noopNewMessage });
  const title = contact?.title || '选择联系人';
  return (
    <section className="grid min-h-0 grid-rows-[auto_1fr_auto] overflow-hidden bg-card">
      <ChatHeader account={account} contact={contact} connection={connection} loading={loading} />
      <div className="h-full min-h-0">
        <AssistantRuntimeProvider runtime={runtime}>
          <ThreadPrimitive.Root className="h-full min-h-0">
            <ThreadPrimitive.Viewport autoScroll className="h-full min-h-0 space-y-3 overflow-y-auto bg-[#f6f8fb] p-5">
              <ThreadPrimitive.Empty><EmptyConversation title={title} /></ThreadPrimitive.Empty>
              <ThreadPrimitive.Messages>{() => <BubbleMessage />}</ThreadPrimitive.Messages>
            </ThreadPrimitive.Viewport>
          </ThreadPrimitive.Root>
        </AssistantRuntimeProvider>
      </div>
      <footer className={`border-t border-border px-5 py-3 text-xs ${error ? 'text-destructive' : 'text-muted-foreground'}`}>{error || '只读消息流；发送接口待接入。'}</footer>
    </section>
  );
}

function ChatHeader({ account, contact, connection, loading }: { account: WAAccount; contact?: WaContact; connection?: LongConnectionState; loading: boolean }) {
  return (
    <header className="flex h-16 items-center justify-between gap-3 border-b border-border px-5">
      <div className="flex min-w-0 items-center gap-3">
        <span className="grid size-10 place-items-center rounded-full bg-emerald-50"><WhatsAppIcon className="size-7" /></span>
        <div className="min-w-0">
          <h2 className="truncate text-sm font-semibold">{contact?.title || '暂无联系人'}</h2>
          <p className="truncate text-xs text-muted-foreground">{contact?.subtitle || waAccountTitle(account)}</p>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <WaConnectionDot connection={connection} loading={loading} />
        {contact && <Badge variant="outline">{contact.count}</Badge>}
        {loading && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
        <Link className="inline-flex size-9 items-center justify-center rounded-lg transition hover:bg-muted" to={waAccountPath(waAccountID(account))} title="账号信息" aria-label="账号信息"><Info size={16} /></Link>
      </div>
    </header>
  );
}

function BubbleMessage() {
  const meta = useMessage((message) => message.metadata.custom as WaChatMeta | undefined);
  const outgoing = Boolean(meta?.outgoing);
  return (
    <MessagePrimitive.Root className={`flex w-full ${outgoing ? 'justify-end' : 'justify-start'}`}>
      <div className={`max-w-[min(640px,82%)] rounded-3xl border px-4 py-3 shadow-sm ${outgoing ? 'rounded-tr-md border-emerald-200 bg-emerald-50' : 'rounded-tl-md border-border bg-card'}`}>
        <div className="mb-1 flex items-center gap-2 text-[11px] text-muted-foreground"><span>{meta?.source || '消息'}</span><span>·</span><MessageTime /></div>
        <div className="flex items-start gap-3">
          <WaMessageContent text={meta?.displayText || ''} />
          {meta?.copyText && <CopyButton text={meta.copyText} />}
        </div>
      </div>
    </MessagePrimitive.Root>
  );
}

function MessageTime() {
  const createdAt = useMessage((message) => message.createdAt);
  return createdAt ? <time>{createdAt.toLocaleString()}</time> : null;
}

function CopyButton({ text }: { text: string }) {
  return <button className="inline-flex size-7 items-center justify-center rounded-full text-muted-foreground transition hover:bg-muted hover:text-foreground" type="button" title="复制" aria-label="复制" onClick={() => void navigator.clipboard?.writeText(text)}><Copy size={14} /></button>;
}

function EmptyConversation({ title }: { title: string }) {
  return <div className="mx-auto mt-16 max-w-sm rounded-2xl bg-card/90 p-6 text-center text-sm text-muted-foreground shadow-sm"><WhatsAppIcon className="mx-auto mb-3 size-9" /><p className="font-medium text-foreground">{title}</p><p className="mt-1">选择联系人或等待新消息。</p></div>;
}

async function noopNewMessage(_message: AppendMessage) {}
