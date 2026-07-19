"use client";

import { useEffect, useRef } from "react";
import { useReducedMotion } from "framer-motion";

/*
  Living hero background: soft glowing motes (gold / sage / warm) drifting upward
  with gentle horizontal sway — visible premium motion, not a rave. Canvas +
  requestAnimationFrame, DPR-aware, pauses when the tab is hidden, and renders a
  single static frame when the user prefers reduced motion.
*/
export function HeroParticles() {
  const ref = useRef<HTMLCanvasElement>(null);
  const reduce = useReducedMotion();

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    const colors = ["199,154,70", "157,180,137", "227,192,121"];
    let w = 0;
    let h = 0;
    let raf = 0;

    type P = {
      x: number;
      y: number;
      r: number;
      vx: number;
      vy: number;
      c: string;
      a: number;
      sway: number;
      phase: number;
    };
    let parts: P[] = [];

    const spawn = (initial: boolean): P => {
      const c = colors[Math.floor(Math.random() * colors.length)];
      return {
        x: Math.random() * w,
        y: initial ? Math.random() * h : h + 20,
        r: 1 + Math.random() * 3.6,
        vx: (Math.random() - 0.5) * 0.25,
        vy: -(0.25 + Math.random() * 0.7),
        c,
        a: (0.18 + Math.random() * 0.55) * 0.85,
        sway: 0.4 + Math.random() * 1.1,
        phase: Math.random() * Math.PI * 2,
      };
    };

    const resize = () => {
      const rect = canvas.getBoundingClientRect();
      w = rect.width;
      h = rect.height;
      canvas.width = Math.floor(w * dpr);
      canvas.height = Math.floor(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      const count = Math.min(76, Math.max(29, Math.floor((w * h) / 15300)));
      parts = Array.from({ length: count }, () => spawn(true));
    };

    const paint = (t: number) => {
      ctx.clearRect(0, 0, w, h);
      ctx.globalCompositeOperation = "lighter";
      for (const p of parts) {
        if (!reduce) {
          p.y += p.vy;
          p.x += p.vx + Math.sin(t * 0.0006 + p.phase) * p.sway * 0.15;
          if (p.y < -20) Object.assign(p, spawn(false));
          if (p.x < -20) p.x = w + 20;
          if (p.x > w + 20) p.x = -20;
        }
        const rad = p.r * 6;
        const g = ctx.createRadialGradient(p.x, p.y, 0, p.x, p.y, rad);
        g.addColorStop(0, `rgba(${p.c},${p.a})`);
        g.addColorStop(1, `rgba(${p.c},0)`);
        ctx.fillStyle = g;
        ctx.beginPath();
        ctx.arc(p.x, p.y, rad, 0, Math.PI * 2);
        ctx.fill();
      }
      ctx.globalCompositeOperation = "source-over";
    };

    const loop = (t: number) => {
      paint(t);
      raf = requestAnimationFrame(loop);
    };

    resize();
    if (reduce) {
      paint(0);
    } else {
      raf = requestAnimationFrame(loop);
    }

    const onVisibility = () => {
      if (document.hidden) {
        cancelAnimationFrame(raf);
      } else if (!reduce) {
        raf = requestAnimationFrame(loop);
      }
    };

    window.addEventListener("resize", resize);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener("resize", resize);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [reduce]);

  return (
    <canvas
      ref={ref}
      aria-hidden
      className="absolute inset-0 z-0 h-full w-full"
    />
  );
}
