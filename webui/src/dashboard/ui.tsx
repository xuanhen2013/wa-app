import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from 'react';
import { useState } from 'react';
import { Loader2 } from 'lucide-react';

export type ResultTone = 'ok' | 'warn' | 'bad' | 'idle';
export type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline';

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'default' | 'outline' | 'ghost' | 'destructive'; size?: 'sm' | 'md' };

export function Button({ className = '', variant = 'default', size = 'md', ...props }: ButtonProps) {
  const variants = {
    default: 'bg-primary text-primary-foreground hover:bg-primary/90',
    outline: 'border border-border bg-background hover:bg-muted',
    ghost: 'hover:bg-muted',
    destructive: 'bg-destructive text-white hover:bg-destructive/90',
  };
  const sizes = { sm: 'h-8 px-3 text-xs', md: 'h-10 px-4 text-sm' };
  return <button className={`inline-flex items-center justify-center gap-2 rounded-lg font-medium transition disabled:pointer-events-none disabled:opacity-50 ${variants[variant]} ${sizes[size]} ${className}`} {...props} />;
}

export function Badge({ variant = 'secondary', children }: { variant?: BadgeVariant; children: ReactNode }) {
  const variants = {
    default: 'bg-primary text-primary-foreground',
    secondary: 'bg-muted text-foreground',
    destructive: 'bg-destructive text-white',
    outline: 'border border-border text-muted-foreground',
  };
  return <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${variants[variant]}`}>{children}</span>;
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`h-10 rounded-lg border border-border bg-background px-3 text-sm outline-none focus:border-primary disabled:opacity-60 ${props.className || ''}`} />;
}

export function Field({ children }: { children: ReactNode }) {
  return <label className="grid gap-1 text-sm">{children}</label>;
}

export function FieldLabel({ children }: { children: ReactNode }) {
  return <span className="font-medium text-foreground">{children}</span>;
}

export function FieldDescription({ children }: { children: ReactNode }) {
  return <p className="text-xs text-muted-foreground">{children}</p>;
}

export function FieldGroup({ children }: { children: ReactNode }) {
  return <div className="grid gap-3">{children}</div>;
}

export function Alert({ children }: { children: ReactNode }) {
  return <div className="rounded-xl border border-border bg-muted/40 p-3 text-sm">{children}</div>;
}

export function AlertDescription({ children }: { children: ReactNode }) {
  return <p className="text-muted-foreground">{children}</p>;
}

export function LoadingText({ children }: { children: ReactNode }) {
  return <span className="inline-flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{children}</span>;
}

export function useToastMessage() {
  const [toast, setToast] = useState<{ tone: ResultTone; message: string } | null>(null);
  const show = (tone: ResultTone, message: string) => setToast({ tone, message });
  return { toast, showOK: (message: string) => show('ok', message), showError: (value: unknown) => show('bad', value instanceof Error ? value.message : String(value)) };
}

export function ToastMessage({ toast }: { toast: { tone: ResultTone; message: string } | null }) {
  if (!toast) return null;
  const tone = toast.tone === 'bad' ? 'border-destructive text-destructive' : 'border-primary text-foreground';
  return <div className={`fixed right-4 top-4 z-50 rounded-xl border bg-card px-4 py-3 text-sm shadow-lg ${tone}`}>{toast.message}</div>;
}

export function Modal({ open, title, children, onClose }: { open: boolean; title: string; children: ReactNode; onClose: () => void }) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-40 grid place-items-center bg-black/30 p-4">
      <section className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-3xl border border-border bg-background p-4 shadow-2xl">
        <header className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-base font-semibold">{title}</h2>
          <button className="grid size-8 place-items-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground" type="button" aria-label="关闭" onClick={onClose}>×</button>
        </header>
        {children}
      </section>
    </div>
  );
}
