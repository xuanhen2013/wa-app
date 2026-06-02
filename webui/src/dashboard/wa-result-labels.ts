export function oldDeviceLabel(value?: boolean, flow?: string) {
  if (value === true || flow === 'registered') return '可用';
  if (flow === 'blocked') return '不可用';
  return '未知';
}

export function booleanLabel(value?: boolean) {
  if (value === true) return '是';
  if (value === false) return '否';
  return '未知';
}

export function smsLabel(available?: boolean, waitSeconds?: number | null) {
  if (waitSeconds && waitSeconds > 0) return `冷却 ${formatSeconds(waitSeconds)}`;
  if (available === true) return '可发';
  if (available === false) return '不可发';
  return '未知';
}

export function cooldownLabel(value?: number | null) {
  return value && value > 0 ? `冷却 ${formatSeconds(value)}` : '';
}

export function methodStateLabel(available?: boolean, cooldownSeconds?: number | null) {
  const cooldown = cooldownLabel(cooldownSeconds);
  if (cooldown) return cooldown;
  if (available === true) return '可用';
  if (available === false) return '不可用';
  return '未知';
}

export function methodLabels(value: unknown) {
  const raw = Array.isArray(value) ? value.map(textOf) : textOf(value).split(',');
  const seen = new Set<string>();
  return raw.map(methodLabel).filter((label) => label && !seen.has(label) && seen.add(label));
}

function formatSeconds(value: number) {
  if (value < 60) return `${Math.ceil(value)}s`;
  const minutes = Math.ceil(value / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.ceil(minutes / 60)}h`;
}

export function methodLabel(value: string) {
  const normalized = value.trim().toUpperCase().replace(/^VERIFICATION_DELIVERY_METHOD_/, '');
  if (!normalized || normalized === 'UNSPECIFIED') return '';
  if (normalized === 'SEND_SMS' || normalized === 'SEND_SMS_TO_WA') return '发送 SMS 至 WA';
  if (normalized === 'SMS') return 'SMS';
  if (normalized === 'VOICE') return '语音';
  if (normalized === 'IN_APP_MESSAGE' || normalized === 'WA_OLD' || normalized === 'OLD_WA') return '旧设备';
  if (normalized === 'PASSKEY') return 'Passkey';
  if (normalized === 'EMAIL' || normalized === 'EMAIL_OTP') return '邮箱';
  if (normalized === 'FLASH') return 'Flash';
  return normalized.replaceAll('_', ' ').toLowerCase().replace(/\b\w/g, (char) => char.toUpperCase());
}

function textOf(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}
