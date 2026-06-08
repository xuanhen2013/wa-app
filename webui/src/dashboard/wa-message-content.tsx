import { ExternalLink, FileText, Image, ListChecks, MapPin, Package, Smile, UserRound } from 'lucide-react';

const RICH_PREFIXES = [
  { prefix: '[商品]', label: '商品', icon: Package },
  { prefix: '[模板]', label: '模板消息', icon: ListChecks },
  { prefix: '[模板回复]', label: '模板回复', icon: ListChecks },
  { prefix: '[图片]', label: '图片', icon: Image },
  { prefix: '[视频]', label: '视频', icon: Image },
  { prefix: '[圆形视频]', label: '圆形视频', icon: Image },
  { prefix: '[文件]', label: '文件', icon: FileText },
  { prefix: '[语音]', label: '语音', icon: FileText },
  { prefix: '[联系人]', label: '联系人', icon: UserRound },
  { prefix: '[位置]', label: '位置', icon: MapPin },
  { prefix: '[实时位置]', label: '实时位置', icon: MapPin },
  { prefix: '[按钮]', label: '按钮', icon: ListChecks },
  { prefix: '[按钮回复]', label: '按钮回复', icon: ListChecks },
  { prefix: '[互动]', label: '互动消息', icon: ListChecks },
  { prefix: '[互动回复]', label: '互动回复', icon: ListChecks },
  { prefix: '[列表]', label: '列表', icon: ListChecks },
  { prefix: '[列表回复]', label: '列表回复', icon: ListChecks },
  { prefix: '[投票]', label: '投票', icon: ListChecks },
  { prefix: '[订单]', label: '订单', icon: Package },
  { prefix: '[回应]', label: '回应', icon: Smile },
];

export function WaMessageContent({ text }: { text: string }) {
  const rich = parseRichMessage(text);
  if (!rich) return <MessageLines text={text} />;
  const Icon = rich.icon;
  return (
    <div className="min-w-0 space-y-2">
      <div className="inline-flex items-center gap-2 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700">
        <Icon size={14} />
        <span>{rich.label}</span>
      </div>
      {rich.body ? <MessageLines text={rich.body} /> : null}
    </div>
  );
}

function MessageLines({ text }: { text: string }) {
  const lines = text.split(/\r?\n/).filter((line) => line.trim() !== '');
  const firstURL = firstLink(lines);
  if (lines.length === 0) return <p className="text-sm text-muted-foreground">空消息</p>;
  return (
    <div className="space-y-1 text-sm leading-6 text-foreground">
      {lines.map((line, index) => (
        <p className="break-words" key={`${index}-${line}`}>
          {linkify(line)}
        </p>
      ))}
      {firstURL ? <LinkPreview url={firstURL} /> : null}
    </div>
  );
}

function linkify(line: string) {
  const tokens = line.split(/(https?:\/\/\S+)/g);
  return tokens.map((token, index) => {
    if (!/^https?:\/\//.test(token)) return token;
    const url = trimLink(token);
    return (
      <a className="text-emerald-700 underline underline-offset-2" href={url} key={`${index}-${token}`} rel="noreferrer" target="_blank">
        {url}
      </a>
    );
  });
}

function LinkPreview({ url }: { url: string }) {
  const host = linkHost(url);
  return (
    <a className="mt-2 flex max-w-sm items-center gap-3 rounded-2xl border border-emerald-100 bg-emerald-50/70 p-3 text-emerald-900 transition hover:border-emerald-200 hover:bg-emerald-50" href={url} rel="noreferrer" target="_blank">
      <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-white text-emerald-700">
        <ExternalLink size={16} />
      </span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium">{host || '打开链接'}</span>
        <span className="block truncate text-xs text-emerald-700/80">{url}</span>
      </span>
    </a>
  );
}

function parseRichMessage(text: string) {
  const value = text.trim();
  for (const item of RICH_PREFIXES) {
    if (value === item.prefix) return { ...item, body: '' };
    if (value.startsWith(`${item.prefix} `)) return { ...item, body: value.slice(item.prefix.length).trim() };
  }
  return null;
}

function firstLink(lines: string[]) {
  for (const line of lines) {
    const match = line.match(/https?:\/\/\S+/);
    if (match?.[0]) return trimLink(match[0]);
  }
  return '';
}

function linkHost(url: string) {
  try {
    return new URL(url).host.replace(/^www\./, '');
  } catch {
    return '';
  }
}

function trimLink(url: string) {
  return url.replace(/[),.;!?]+$/, '');
}
