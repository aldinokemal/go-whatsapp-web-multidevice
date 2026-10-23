---
name: whatsapp-cs-closing
description: "Use when closing WhatsApp CS chats through follow-up."
version: 1.0.0
author: Ruang Sanad
license: Proprietary
metadata:
  hermes:
    tags: [whatsapp, customer-service, sales, closing, follow-up]
    related_skills: [wavio-cs-knowledge-base, wa-copywriter-humanizer]
---

# WhatsApp CS Closing

## When to Use

Use for inbound WhatsApp sales/CS: responding to enquiries, qualifying needs, recommending the right option, handling objections, closing registration/payment, auditing transcripts, and building follow-up sequences.

For Ruang Sanad, preserve a warm, safe, non-judgmental Islamic educational voice. Optimize for accurate matching and trust, not forced conversion.

## Procedure

1. **Establish the fact inventory before drafting.** Inspect the actual conversation and current verified business material. Separate confirmed facts (program, process, price, form, policy) from variables that must be checked live (schedule, slot, start date, teacher, payment instructions, deadline). Put unverified items in `[PLACEHOLDERS]`; never fill a gap from memory.
2. **Classify the lead before choosing a script.** Capture intent, gender/segment if relevant, prior learning, desired outcome, timing, primary objection, conversation stage, and permission to follow up. Use a CRM field for each, plus one explicit next action/date.
3. **Answer the question asked, then discover.** Respond to a price or schedule question directly. Follow it with no more than one or two decision-relevant questions; do not bury an answer behind a long introduction or interrogate the lead.
4. **Use CLEAR for every live path:** Context → Listen → Explain a need-matched option → Ask one concrete next step → Respect autonomy. Summarize the need before recommending an option.
5. **Recommend, do not dump a catalogue.** Offer one best-fit option and, only when useful, one comparable fallback. Tie each recommendation to the lead's stated need. Cap visible choices at three.
6. **Trial-close before form/payment.** Ask what remains unclear. If there is an objection, clarify the root cause before responding; do not send a payment link to someone who has not resolved their concern.
7. **Close with explicit verification.** Before payment, restate class, schedule, frequency, and total. Use only the official form/payment channel. After payment, say only that proof was received until verification is complete; confirm enrollment only after a verified status.
8. **Follow up by intent, not blindly.** Use the 0/1/3/7/14/30 rhythm in `references/follow-up-and-objection-playbook.md`: Day 7 requires a real new value; Day 30 requires a valid opt-in or a clearly relevant update. Stop the sequence immediately on a refusal, opt-out, complaint, or no permission.
9. **Audit before sending.** Check facts, one CTA, respectful wording, live placeholders replaced, CRM status updated, and no technical/internal content exposed.

## Objection handling

Use LURC: **Listen → Understand → Respond → Confirm**. Treat an objection as information, not something to defeat. Ask one clarifying question to distinguish price, timing, fit, trust, permission, or low priority; answer only with available facts/options; then confirm whether the concern is resolved.

Use the reusable response branches in `references/follow-up-and-objection-playbook.md` for price, timing, consistency, current study, online learning, family discussion, trust/teacher questions, payment readiness, and polite deferral.

## Ruang Sanad communication rules

- State factual learning outcomes and process, not guarantees of speed, fluency, or religious outcome.
- Do not use guilt, fear, spiritual pressure, invented urgency, false scarcity, fake testimonial, or hidden price to close.
- Do not claim a teacher's credential, class size, schedule, availability, missed-class policy, promotion, or payment method without verifying it for the exact class.
- Do not display AI errors, rate limits, internal workflows, or system issues to a lead. Route failures to a human fallback or send an honest short delay notice without technical detail.
- Respect `belum dulu`, `tidak`, `stop`, or an expression that follow-up is unwelcome: send one concise closure if needed, set do-not-follow-up, and do not reactivate without fresh permission.
- Keep WhatsApp copy conversational and short. Do not deliberately introduce typos; clarity and warmth outrank artificial informality.

## Script architecture

Build reusable scripts as modular messages rather than one long template:

1. greeting + permission/context;
2. direct answer to the stated question;
3. discovery question;
4. need summary;
5. recommendation + reason;
6. trial close;
7. registration/payment confirmation;
8. conditional follow-up branch;
9. respectful closure.

Use `[NAMA]`, `[KEBUTUHAN]`, `[KELAS]`, `[JADWAL]`, `[FREKUENSI]`, `[HARGA]`, `[SLOT]`, `[LINK_FORM]`, `[METODE_PEMBAYARAN]`, and `[WAKTU_BALAS]` placeholders. Every placeholder must be resolved or the message must say the admin will check it.

## Quality gate

Before delivering a script library or audit, verify it includes:

- a first-response path for general, price, schedule, beginner, and already-learning leads;
- discovery and recommendation paths;
- form, payment, and post-payment wording with verification boundaries;
- LURC-based objection branches;
- a conditional 30-day follow-up sequence with hard stop rules;
- CRM tags/next-action behavior and escalation triggers;
- no unsupported business facts or coercive tactics.

## Reference

Use `references/follow-up-and-objection-playbook.md` for the cadence decision table, stop conditions, and modular response patterns.