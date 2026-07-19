"use client";

import { Fragment } from "react";
import { motion, useReducedMotion, type Variants } from "framer-motion";

/*
  The client formula from the deck — цель → деньги → риски → переговоры → ход —
  as an animated chain of gold-edged nodes joined by drawing connectors.
*/
const steps = ["Цель", "Деньги", "Риски", "Переговоры", "Следующий ход"];

const container: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.14, delayChildren: 0.1 } },
};
const nodeV: Variants = {
  hidden: { opacity: 0, y: 14, scale: 0.9 },
  show: {
    opacity: 1,
    y: 0,
    scale: 1,
    transition: { duration: 0.45, ease: [0.22, 1, 0.36, 1] },
  },
};
const linkV: Variants = {
  hidden: { opacity: 0, scaleX: 0 },
  show: { opacity: 1, scaleX: 1, transition: { duration: 0.35 } },
};

export function LogicChain() {
  const reduce = useReducedMotion();
  return (
    <motion.div
      variants={container}
      initial={reduce ? undefined : "hidden"}
      whileInView={reduce ? undefined : "show"}
      viewport={{ once: true, margin: "-80px" }}
      className="flex flex-wrap items-center justify-center gap-x-3 gap-y-4"
    >
      {steps.map((s, i) => {
        const last = i === steps.length - 1;
        return (
          <Fragment key={s}>
            <motion.span
              variants={reduce ? undefined : nodeV}
              className={`inline-flex items-center rounded-full border px-5 py-2.5 font-sans text-sm font-medium ${
                last
                  ? "border-gold/50 bg-raised text-gold shadow-[0_10px_30px_-14px_rgba(199,154,70,0.6)]"
                  : "border-edge/15 bg-surface text-ink"
              }`}
            >
              {s}
            </motion.span>
            {!last && (
              <motion.span
                aria-hidden
                variants={reduce ? undefined : linkV}
                className="h-px w-7 origin-left bg-gradient-to-r from-gold/70 to-gold/30 sm:w-10"
              />
            )}
          </Fragment>
        );
      })}
    </motion.div>
  );
}
