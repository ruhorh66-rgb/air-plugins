import Link from "next/link";
import { HeroVideo } from "./HeroVideo";

function ArrowUR({ className = "h-4 w-4" }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className={className}
    >
      <path d="M7 17 17 7M8 7h9v9" />
    </svg>
  );
}

/*
  Cinematic closing CTA — full-width motion video, frosted glass primary button,
  solid gold secondary. Adapted from the MotionSites "CTA + Footer" pattern to the
  KEG dark-evergreen palette and content. The site's real <Footer> follows.
*/
export function CtaSection() {
  return (
    <section className="relative overflow-hidden px-6 py-28 text-center sm:py-36">
      <HeroVideo
        src="/cta.mp4"
        className="absolute inset-0 z-0 h-full w-full object-cover"
      />
      {/* fades into the page background + legibility tint */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 z-[1] h-52"
        style={{ background: "linear-gradient(to bottom, #0f140f, transparent)" }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 bottom-0 z-[1] h-52"
        style={{ background: "linear-gradient(to top, #0f140f, transparent)" }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 z-[1]"
        style={{
          background:
            "radial-gradient(120% 95% at 50% 50%, rgba(15,20,15,0.3), rgba(15,20,15,0.72))",
        }}
      />

      <div className="relative z-10 mx-auto max-w-3xl">
        <p className="mb-6 flex items-center justify-center gap-3 font-sans text-xs font-semibold uppercase tracking-[0.24em] text-sage">
          <span aria-hidden className="h-px w-8 bg-gold" />
          Первый шаг
          <span aria-hidden className="h-px w-8 bg-gold" />
        </p>
        <h2 className="font-serif text-4xl font-bold leading-[1.04] text-ink sm:text-5xl lg:text-6xl">
          Верните конфликт в{" "}
          <span className="text-gold-grad">управляемую плоскость</span>
        </h2>
        <p className="mx-auto mt-6 max-w-xl text-lg text-muted">
          Разберём позицию, покажем следующий ход и возьмём рабочее ведение на
          себя. Без втягивания в спор — до суда и без лишней эскалации.
        </p>
        <div className="mt-11 flex flex-col items-center justify-center gap-4 sm:flex-row">
          <Link
            href="/contact"
            className="liquid-glass-strong inline-flex items-center gap-2 rounded-full px-7 py-3.5 font-sans text-base font-medium text-ink transition-colors hover:bg-white/10"
          >
            Обсудить ситуацию
            <ArrowUR className="h-5 w-5" />
          </Link>
          <Link
            href="/service"
            className="inline-flex items-center gap-2 rounded-full bg-gradient-to-b from-[#e3c079] to-gold px-7 py-3.5 font-sans text-base font-semibold text-[#12160f] shadow-[0_12px_34px_-12px_rgba(199,154,70,0.6)] transition-transform hover:-translate-y-0.5"
          >
            Как это работает
            <ArrowUR />
          </Link>
        </div>
      </div>
    </section>
  );
}
