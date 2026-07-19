import Link from "next/link";
import type { ReactNode } from "react";

type Variant = "primary" | "secondary";

const base =
  "group relative inline-flex items-center justify-center gap-2.5 overflow-hidden rounded-full px-8 py-4 text-[0.95rem] font-sans font-semibold tracking-wide transition-all duration-300 active:translate-y-0 active:scale-[0.98] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-gold focus-visible:ring-offset-page";

const variants: Record<Variant, string> = {
  // Gold — the premium signal. Dark ink text on gold for AA contrast.
  primary:
    "bg-gradient-to-b from-[#e3c079] to-gold text-[#12160f] shadow-[0_10px_30px_-12px_rgba(199,154,70,0.6)] hover:shadow-[0_16px_40px_-12px_rgba(199,154,70,0.75)] hover:-translate-y-0.5",
  secondary:
    "border border-edge/20 text-ink hover:border-gold/60 hover:text-gold bg-white/[0.02]",
};

export function CTAButton({
  href,
  children,
  variant = "primary",
  className = "",
}: {
  href: string;
  children: ReactNode;
  variant?: Variant;
  className?: string;
}) {
  return (
    <Link href={href} className={`${base} ${variants[variant]} ${className}`}>
      {/* light sheen sweep on hover */}
      <span
        aria-hidden
        className="pointer-events-none absolute inset-0 -translate-x-full bg-gradient-to-r from-transparent via-white/25 to-transparent transition-transform duration-700 ease-out group-hover:translate-x-full"
      />
      <span className="relative">{children}</span>
      <span
        aria-hidden
        className="relative transition-transform duration-300 group-hover:translate-x-1"
      >
        →
      </span>
    </Link>
  );
}
