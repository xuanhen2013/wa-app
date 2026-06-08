import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Navigate } from 'react-router';
import type { LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { getWaAccountOtpMessages, getWaContacts, getWaMessages, waAccountID, waKeys } from './wa-api';
import { buildWaChatEvents, buildWaContacts, filterWaEvents } from './wa-chat-model';
import { WaChatThread } from './wa-chat-thread';
import { WaContactList } from './wa-contact-list';
import { waContactPath } from './wa-route-paths';

export function WaInbox({ account, connection, contactID }: { account: WAAccount; connection?: LongConnectionState; contactID: string }) {
  const accountID = waAccountID(account);
  const messagesQuery = useQuery({ queryKey: waKeys.messages(accountID), queryFn: () => getWaMessages(accountID), enabled: Boolean(accountID), refetchInterval: 8000 });
  const otpQuery = useQuery({ queryKey: waKeys.otpMessages(accountID), queryFn: () => getWaAccountOtpMessages(accountID), enabled: Boolean(accountID), refetchInterval: 10000 });
  const contactsQuery = useQuery({ queryKey: waKeys.contacts(accountID), queryFn: () => getWaContacts(accountID), enabled: Boolean(accountID), refetchInterval: 30000 });
  const events = useMemo(() => buildWaChatEvents(messagesQuery.data?.messages || [], otpQuery.data?.otp_messages || []), [messagesQuery.data?.messages, otpQuery.data?.otp_messages]);
  const contacts = useMemo(() => buildWaContacts(events, contactsQuery.data?.contacts || []), [events, contactsQuery.data?.contacts]);
  const activeContactID = contacts.some((contact) => contact.id === contactID) ? contactID : contacts[0]?.id || '';
  const activeContact = contacts.find((contact) => contact.id === activeContactID);
  const threadEvents = useMemo(() => filterWaEvents(events, activeContactID), [events, activeContactID]);
  const error = messagesQuery.data?.error?.message || otpQuery.data?.error?.message || contactsQuery.data?.error?.message;
  if (activeContactID && activeContactID !== contactID) return <Navigate to={waContactPath(accountID, activeContactID)} replace />;
  return (
    <section className="grid h-dvh min-h-0 md:grid-cols-[320px_minmax(0,1fr)]">
      <WaContactList accountID={accountID} contacts={contacts} selectedID={activeContactID} loading={messagesQuery.isLoading || otpQuery.isLoading || contactsQuery.isLoading} error={error} />
      <WaChatThread account={account} connection={connection} contact={activeContact} events={threadEvents} loading={messagesQuery.isFetching || otpQuery.isFetching || contactsQuery.isFetching} error={error} />
    </section>
  );
}
