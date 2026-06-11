import type { SyntheticEvent } from 'react';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { waAccountProfilePictureURL, waAccountTitle } from './wa-api';
import { WhatsAppIcon } from './wa-brand-icon';

export function WaAccountAvatar({ account, version, size = 'md' }: { account: WAAccount; version: string; size?: 'xs' | 'sm' | 'md' | 'lg' }) {
  const src = waAccountProfilePictureURL(account, version || 'latest');
  const sizeClass = size === 'xs' ? 'size-8' : size === 'sm' ? 'size-9' : size === 'lg' ? 'size-12' : 'size-10';
  const iconClass = size === 'xs' ? 'size-5!' : size === 'sm' ? 'size-6!' : size === 'lg' ? 'size-8!' : 'size-7!';
  const hideBrokenImage = (event: SyntheticEvent<HTMLImageElement>) => { event.currentTarget.style.display = 'none'; };
  return (
    <span className={`relative grid ${sizeClass} shrink-0 place-items-center overflow-hidden rounded-full bg-emerald-50`}>
      <WhatsAppIcon className={iconClass} title={waAccountTitle(account)} />
      {src ? <img key={src} className="absolute inset-0 size-full object-cover" src={src} alt={waAccountTitle(account)} onError={hideBrokenImage} /> : null}
    </span>
  );
}
