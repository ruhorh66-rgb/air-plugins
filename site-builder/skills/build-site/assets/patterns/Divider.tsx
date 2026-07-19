/* Thin gold accent divider with a centered diamond — replaces plain borders. */
export function Divider() {
  return (
    <div className="mx-auto flex w-full max-w-6xl items-center gap-4 px-5 sm:px-8">
      <span className="h-px flex-1 bg-gradient-to-r from-transparent to-gold/40" />
      <span
        aria-hidden
        className="h-2 w-2 rotate-45 border border-gold/60 bg-gold/20"
      />
      <span className="h-px flex-1 bg-gradient-to-l from-transparent to-gold/40" />
    </div>
  );
}
