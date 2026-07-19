"use client";

import { useEffect, useRef } from "react";
import { useReducedMotion } from "framer-motion";

/*
  Full-bleed background video for the hero. Muted / looped / inline like a
  motion backdrop. Pauses (shows a still frame) when the user prefers reduced
  motion.
*/
export function HeroVideo({
  src,
  className = "",
}: {
  src: string;
  className?: string;
}) {
  const ref = useRef<HTMLVideoElement>(null);
  const reduce = useReducedMotion();

  useEffect(() => {
    const v = ref.current;
    if (!v) return;
    if (reduce) v.pause();
    else v.play().catch(() => {});
  }, [reduce]);

  return (
    <video
      ref={ref}
      autoPlay={!reduce}
      muted
      loop
      playsInline
      preload="auto"
      aria-hidden
      className={className}
    >
      <source src={src} type="video/mp4" />
    </video>
  );
}
