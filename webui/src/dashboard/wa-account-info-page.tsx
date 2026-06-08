import { ArrowLeft } from 'lucide-react';
import { Link } from 'react-router';
import type { ClientProfile, WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { waAccountID, waAccountTitle } from './wa-api';
import { WaAccountDetail } from './wa-account-detail';
import { WhatsAppIcon } from './wa-brand-icon';
import { waChatsPath } from './wa-route-paths';

export function WaAccountInfoPage({ account, profiles, profilesLoading, busy, onDelete, onDone, onError }: { account: WAAccount; profiles: ClientProfile[]; profilesLoading: boolean; busy: boolean; onDelete: (account: WAAccount) => void; onDone: (message: string) => void; onError: (message: string) => void }) {
  return (
    <section className="grid h-dvh min-h-0 grid-rows-[auto_1fr] bg-background">
      <header className="flex h-16 items-center justify-between border-b border-border bg-card px-5">
        <div className="flex min-w-0 items-center gap-3">
          <Link className="inline-flex size-9 items-center justify-center rounded-lg transition hover:bg-muted" to={waChatsPath(waAccountID(account))} title="返回消息" aria-label="返回消息"><ArrowLeft size={16} /></Link>
          <span className="grid size-10 place-items-center rounded-full bg-emerald-50"><WhatsAppIcon className="size-7" /></span>
          <div className="min-w-0"><h1 className="truncate text-base font-semibold">{waAccountTitle(account)}</h1><p className="truncate font-mono text-xs text-muted-foreground">{waAccountID(account)}</p></div>
        </div>
      </header>
      <main className="min-h-0 overflow-y-auto p-6">
        <div className="mx-auto max-w-3xl rounded-3xl border border-border bg-card p-5 shadow-sm">
          <WaAccountDetail account={account} profiles={profiles} profilesLoading={profilesLoading} busy={busy} onDelete={onDelete} onDone={onDone} onError={onError} />
        </div>
      </main>
    </section>
  );
}
