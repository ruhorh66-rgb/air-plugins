"use client";

import { motion, useReducedMotion, type Variants } from "framer-motion";

/*
  Hero headline that assembles itself word-by-word (fade + rise + de-blur, staggered).
  Two lines, the second in the gold gradient. Falls back to a static headline when
  the user prefers reduced motion.
*/
const L1 = "Управляемое закрытие".split(" ");
const L2 = "сложных B2B-конфликтов".split(" ");

const wordCls = "mr-[0.24em] inline-block";
const lineCls = "block text-[clamp(2.4rem,7vw,5.5rem)] leading-[1.03]";

const container: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.08, delayChildren: 0.12 } },
};
const wordV: Variants = {
  hidden: { opacity: 0, y: 26, filter: "blur(8px)" },
  show: {
    opacity: 1,
    y: 0,
    filter: "blur(0px)",
    transition: { duration: 0.7, ease: [0.22, 1, 0.36, 1] },
  },
};

export function AnimatedHeadline() {
  const reduce = useReducedMotion();

  if (reduce) {
    return (
      <h1 className="font-serif font-bold text-ink [text-wrap:balance]">
        <span className={lineCls}>Управляемое закрытие</span>
        <span className={`${lineCls} text-gold-grad -mt-1 sm:-mt-2`}>
          сложных B2B-конфликтов
        </span>
      </h1>
    );
  }

  return (
    <motion.h1
      variants={container}
      initial="hidden"
      animate="show"
      className="font-serif font-bold text-ink [text-wrap:balance]"
    >
      <span className={lineCls}>
        {L1.map((w, i) => (
          <motion.span key={i} variants={wordV} className={wordCls}>
            {w}
          </motion.span>
        ))}
      </span>
      <span className={`${lineCls} -mt-1 sm:-mt-2`}>
        {L2.map((w, i) => (
          <motion.span
            key={i}
            variants={wordV}
            className={`${wordCls} text-gold-grad`}
          >
            {w}
          </motion.span>
        ))}
      </span>
    </motion.h1>
  );
}
