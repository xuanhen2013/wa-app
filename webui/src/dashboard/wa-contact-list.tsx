import type { MouseEvent } from 'react';
import { useMemo, useRef, useState } from 'react';
import { Loader2, Search, Trash2 } from 'lucide-react';
import { NavLink } from 'react-router';
import { WAContactKind } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { WaContact } from './wa-chat-model';
import { formatChatTime } from './wa-chat-model';
import { WhatsAppIcon } from './wa-brand-icon';
import { waContactPath } from './wa-route-paths';
import { Badge } from './ui';

export function WaContactList({ accountID, contacts, selectedID, loading, error, deletingID, onOpenContact, onDeleteContact }: { accountID: string; contacts: WaContact[]; selectedID: string; loading: boolean; error?: string; deletingID?: string; onOpenContact: (contactID: string) => void; onDeleteContact: (contactID: string) => void }) {
  const [query, setQuery] = useState('');
  const visibleContacts = useMemo(() => filterContacts(contacts, query), [contacts, query]);
  const unreadCount = contacts.reduce((sum, contact) => sum + contact.unreadCount, 0);
  return (
    <aside className="grid min-h-0 grid-rows-[auto_auto_1fr] overflow-hidden border-r border-border bg-card">
      <header className="flex h-16 items-center justify-between px-4">
        <div><h2 className="text-base font-semibold">联系人</h2><p className="text-xs text-muted-foreground">{contacts.length} 个会话{unreadCount > 0 ? ` · ${unreadCount} 条未读` : ''}</p></div>
        {loading && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
      </header>
      <div className="px-3 pb-3">
        <label className="flex h-10 items-center gap-2 rounded-xl bg-muted/50 px-3 text-sm text-muted-foreground"><Search size={15} /><input className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-muted-foreground" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索联系人" /></label>
      </div>
      <div className="min-h-0 overflow-y-auto p-2">
        {error && <p className="rounded-xl border border-destructive/30 p-3 text-sm text-destructive">{error}</p>}
        {!loading && !error && contacts.length === 0 && <p className="p-4 text-sm text-muted-foreground">暂无联系人，收到消息后会显示在这里。</p>}
        {!loading && !error && contacts.length > 0 && visibleContacts.length === 0 && <p className="p-4 text-sm text-muted-foreground">没有匹配联系人。</p>}
        {visibleContacts.map((contact) => <ContactRow key={contact.id} accountID={accountID} contact={contact} selected={contact.id === selectedID} deleting={deletingID === contact.id} onOpenContact={onOpenContact} onDeleteContact={onDeleteContact} />)}
      </div>
    </aside>
  );
}

function ContactRow({ accountID, contact, selected, deleting, onOpenContact, onDeleteContact }: { accountID: string; contact: WaContact; selected: boolean; deleting: boolean; onOpenContact: (contactID: string) => void; onDeleteContact: (contactID: string) => void }) {
  const unread = contact.unreadCount > 0;
  const holdTimer = useRef<number | undefined>(undefined);
  const revealedByHold = useRef(false);
  const [deleteVisible, setDeleteVisible] = useState(false);
  const revealDelete = (blockNextClick = true) => {
    revealedByHold.current = blockNextClick;
    setDeleteVisible(true);
  };
  const clearHold = () => window.clearTimeout(holdTimer.current);
  const startHold = () => {
    clearHold();
    holdTimer.current = window.setTimeout(() => revealDelete(), 650);
  };
  const openOrReveal = (event: MouseEvent<HTMLAnchorElement>) => {
    if (revealedByHold.current) {
      event.preventDefault();
      revealedByHold.current = false;
      return;
    }
    if (unread) onOpenContact(contact.id);
  };
  return (
    <div className={`mb-1 grid grid-cols-[1fr_auto] items-center rounded-2xl transition hover:bg-muted/60 ${selected ? 'bg-primary/10' : unread ? 'bg-emerald-50/70' : ''}`} onContextMenu={(event) => { event.preventDefault(); revealDelete(false); }}>
      <NavLink className="grid min-w-0 grid-cols-[42px_1fr_auto] items-center gap-3 px-3 py-2 text-left" to={waContactPath(accountID, contact.id)} title="长按显示删除" onClick={openOrReveal} onPointerDown={startHold} onPointerLeave={clearHold} onPointerCancel={clearHold} onPointerUp={clearHold}>
        <ContactAvatar contact={contact} />
        <span className="min-w-0 space-y-0.5">
          <span className="flex min-w-0 items-center gap-2">
            <span className={`truncate text-sm ${unread ? 'font-semibold text-foreground' : 'font-medium'}`}>{contact.title}</span>
            <ContactKindBadge kind={contact.kind} />
          </span>
          <span className={`block truncate text-xs ${unread ? 'font-medium text-foreground/85' : 'text-foreground/70'}`}>{contact.preview || contact.subtitle}</span>
          {contact.preview && contact.subtitle && <span className="block truncate text-[11px] text-muted-foreground">{contact.subtitle}</span>}
        </span>
        <span className="grid justify-items-end gap-1">
          <time className="text-[11px] text-muted-foreground">{formatChatTime(contact.lastAt)}</time>
          {unread ? <Badge variant="default">{contact.unreadCount}</Badge> : contact.count > 0 ? <span className="text-[11px] text-muted-foreground">{contact.count}</span> : null}
        </span>
      </NavLink>
      {deleteVisible && <button className="mr-2 grid size-8 place-items-center rounded-full text-muted-foreground transition hover:bg-destructive/10 hover:text-destructive disabled:opacity-50" type="button" title="删除联系人" aria-label="删除联系人" disabled={deleting} onClick={() => onDeleteContact(contact.id)}>{deleting ? <Loader2 className="size-4 animate-spin" /> : <Trash2 size={14} />}</button>}
    </div>
  );
}

function ContactAvatar({ contact }: { contact: WaContact }) {
  const [failedURL, setFailedURL] = useState('');
  if (contact.profilePictureURL && failedURL !== contact.profilePictureURL) {
    return <img className="size-10 rounded-full object-cover" src={contact.profilePictureURL} alt={contact.title} loading="lazy" onError={() => setFailedURL(contact.profilePictureURL || '')} />;
  }
  return <span className="grid size-10 place-items-center rounded-full bg-emerald-50"><WhatsAppIcon className="size-6" title={contact.title} /></span>;
}

function ContactKindBadge({ kind }: { kind: WAContactKind }) {
  const label = kindLabel(kind);
  if (!label) return null;
  return <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{label}</span>;
}

function filterContacts(contacts: WaContact[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return contacts;
  return contacts.filter((contact) => `${contact.title} ${contact.subtitle} ${contact.preview} ${contact.id}`.toLowerCase().includes(needle));
}

function kindLabel(kind: WAContactKind) {
  if (kind === WAContactKind.WA_CONTACT_KIND_GROUP) return '群';
  if (kind === WAContactKind.WA_CONTACT_KIND_BUSINESS) return '企';
  if (kind === WAContactKind.WA_CONTACT_KIND_SYSTEM) return '系统';
  if (kind === WAContactKind.WA_CONTACT_KIND_INTEROP) return '互通';
  return '';
}
