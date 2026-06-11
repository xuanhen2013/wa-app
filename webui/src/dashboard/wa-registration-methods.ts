import { RegistrationLoginMethod, VerificationDeliveryMethod } from '../proto/byte/v/forge/waapp/v1/registration';
import { methodLabel } from './wa-result-labels';
import type { VerificationMethodStatus, WaProbeStatus } from './wa-result-model';

export type RegistrationMethodOption = {
  value: VerificationDeliveryMethod | RegistrationLoginMethod;
  code: string;
  label: string;
  description: string;
};
export type SelectableRegistrationMethodOption = Omit<RegistrationMethodOption, 'value'> & {
  value: VerificationDeliveryMethod;
};

export const selectableRegistrationMethods: SelectableRegistrationMethodOption[] = [
  methodOption(VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_SMS, 'sms', '服务端下发短信验证码'),
  methodOption(VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_VOICE, 'voice', '语音电话播报验证码'),
  methodOption(VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_WA_OLD, 'wa_old', '旧设备 / 已登录 WhatsApp 验证'),
  methodOption(VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_EMAIL_OTP, 'email_otp', '邮箱验证码'),
  methodOption(VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_SEND_SMS, 'send_sms', '从本机发送短信到 WhatsApp'),
];

export const apkSupportedLoginRegistrationMethods = [
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_SMS, 'sms', '服务端下发短信验证码'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_VOICE, 'voice', '语音电话播报验证码'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_FLASH, 'flash', 'Flash call / 未接来电验证，需要 Android 设备侧监听'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_WA_OLD, 'wa_old', '旧设备 / 已登录 WhatsApp 验证'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_EMAIL_OTP, 'email_otp', '邮箱验证码'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_SEND_SMS, 'send_sms', '从本机发送短信到 WhatsApp'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_SILENT_AUTH, 'silent_auth', '静默验证专用流程'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_SILENT_AUTH_TS43, 'silent_auth_ts_43', 'TS43 静默验证专用流程'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_AUTOCONF, 'autoconf', '自动确认专用流程'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_DEEPLINK_OTP, 'deeplink_otp', 'Deep Link OTP 专用流程'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_ACCOUNT_TRANSFER, 'acc_tr', '账号迁移验证'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_PASSKEY, 'passkey', 'Passkey 登录'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_DISCOVERABLE_CREDENTIAL, 'discoverable_credential', '可发现凭据登录'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_OAUTH_EMAIL, 'oauth_email', 'OAuth 邮箱验证'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_RECAPTCHA, 'recaptcha', '反滥用 proof'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_TWO_FACTOR_PIN, 'twofac_pin', '两步验证 PIN'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_PASSWORD, 'password', '密码验证'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_WIPE_FULL, 'wipe_full', '完整重置'),
  loginMethodOption(RegistrationLoginMethod.REGISTRATION_LOGIN_METHOD_WIPE_OFFLINE, 'wipe_offline', '离线重置'),
];

export function registrationMethodStatus(status: WaProbeStatus, method: VerificationDeliveryMethod) {
  if (method === VerificationDeliveryMethod.VERIFICATION_DELIVERY_METHOD_SMS) {
    return { available: status.smsAvailable, cooldownSeconds: status.smsWaitSeconds };
  }
  return status.methodStatuses.find((item) => methodStatusMatches(item, method));
}

export function registrationMethodAvailable(status: WaProbeStatus, method: VerificationDeliveryMethod) {
  const methodStatus = registrationMethodStatus(status, method);
  if (!methodStatus) return false;
  if (methodStatus.cooldownSeconds && methodStatus.cooldownSeconds > 0) return false;
  return methodStatus.available === true;
}

function methodOption(value: VerificationDeliveryMethod, code: string, description: string): SelectableRegistrationMethodOption {
  return { value, code, label: methodLabel(code), description };
}

function loginMethodOption(value: RegistrationLoginMethod, code: string, description: string): RegistrationMethodOption {
  return { value, code, label: methodLabel(code), description };
}

function methodStatusMatches(status: VerificationMethodStatus, method: VerificationDeliveryMethod) {
  return status.key === methodLabel(method).toLowerCase() || status.key === methodLabel(methodCode(method)).toLowerCase();
}

function methodCode(method: VerificationDeliveryMethod) {
  return selectableRegistrationMethods.find((item) => item.value === method)?.code || '';
}
