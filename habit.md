# Griffin User Behavioral & Lingo Habits Profile

This document records the user's personal linguistic patterns, mobile typing slips, communication shorthands, contact aliases, device postures, and execution expectations. Griffin's orchestrator, classifier, and device hands adapt to these conventions so crude or shorthand commands work seamlessly without requiring explicit system instructions or configuration from the user.

---

## 1. Colloquial Lingo & Contact Aliases

| Shorthand / Slang | Canonical Term | Interpretation & Routing Invariant |
| :--- | :--- | :--- |
| **`bhai`** | Brother / Primary Contact | Default family recipient for personal communication. Treated as a high-priority contact across WhatsApp and phone calling hands. |
| **`mom`**, **`maa`** | Mother | Family contact alias for personal ops. |
| **`dad`**, **`papa`** | Father | Family contact alias for personal ops. |
| **`wassup`**, **`sup`**, **`yo`** | Casual greeting | Colloquial greeting in Indian/urban English context (e.g. *"message bhai wassup"* means send text "wassup" to bhai). |
| **`hellooo`**, **`heyy`** | Casual greeting | Expressive informal greetings. |

---

## 2. Shorthand Verbs & Mobile Typing Slips

Users typing on mobile keyboards frequently produce character transposition and phonetically shortened verbs. Griffin normalizes these transparently:

| Input Pattern | Canonical Action | Channel Default |
| :--- | :--- | :--- |
| **`sned`**, **`snd`** | `send` | Default messaging hand (WhatsApp) |
| **`msg`** | `message` | Default messaging hand (WhatsApp) |
| **`txt`** | `text` | Default messaging hand (WhatsApp) |
| **`ping`**, **`tell`** | `message` / `inform` | Default messaging hand (WhatsApp) |
| **`watsapp`**, **`wapp`**, **`watsp`**, **`whatsap`** | `whatsapp` | WhatsApp Hand (`com.whatsapp` or WhatsApp Web) |
| **`dm`** | `direct message` | Instagram Hand (`com.instagram.android` / IG Web) |
| **`fwd`** | `forward` | Message forwarding |

---

## 3. Implicit Messaging Defaults

- **Implicit Platform**: When the user requests a message to a person without specifying a platform (e.g., *"message bhai wassup"*, *"text bhai I'm running late"*, *"msg mom that dinner is ready"*), Griffin **defaults to WhatsApp**.
- **Deliverable Guard**: If the request contains a deliverable noun (e.g., *"send a report"*, *"send invoice"*), it routes to file synthesis or email, not interpersonal chat.
- **Engineering Guard**: If the request contains programming verbs or software entities (e.g., *"write a whatsapp bot"*, *"send an api call"*), it routes to engineering, never device hands.

---

## 4. Device Posture & Locked Phone Invariant

### Real-World Mobile Condition
In everyday life, users do **not** leave their phones permanently unlocked or awake with the screen on. The phone is typically locked, in a pocket or on a desk, with power on and connected to mobile data or Wi-Fi.

### Execution Handling
1. **Automated Keyguard Wake & Dismissal**:
   - When an Android hand step runs via ADB, Griffin sends `KEYCODE_WAKEUP` followed by `wm dismiss-keyguard` and `input keyevent 82` (KEYCODE_MENU) to dismiss standard swipe lockscreens.
   - If an optional phone PIN is provided via `GRIFFIN_PHONE_PIN`, ADB inputs the PIN headlessly.
2. **Transparent Laptop Web Fallback**:
   - If the phone has a secure biometric/PIN lock and remains locked, ADB returns `ErrNotReady`.
   - `whatsapp.Messenger` immediately and automatically falls back to **WhatsApp Web** on the laptop.
   - WhatsApp Web runs in a headless Chrome browser session that connects to WhatsApp multi-device cloud servers. It does **not** require the phone's screen to be on or unlocked, requiring only that the phone has battery and active internet (mobile data).
   - The user's task succeeds with zero friction.

---

## 5. Sent Message Verification & Read-Back

- **Delivery Confirmation**: Every send action (`MessagingSend`) waits for real-time confirmation:
  - Verifies the composer box held the exact text before send was clicked.
  - Verifies an outgoing message bubble with identical text appears on the right side of the chat.
  - Verifies the message is not stuck in a "pending/clock" state.
- **Read-Back Skill (`MessagingReadRecent`)**:
  - Reading recent chat messages from a recipient confirms sent messages and fetches replies.
  - Griffin combines send + read-back verification in its integration test suites.

---

## 6. Smart Resource Allocation

- **Google Antigravity CLI (`agy`)**:
  - Web research, search queries, company scraping, and daily routine tasks.
  - Fast message formulation (`wa_compose`) and casual text drafting.
- **Claude Pro CLI (`claude`)**:
  - Deep linguistic reasoning, keyword-optimized resume tailoring, and complex code reviews.
- **S3 Vault Document Caching**:
  - Working document files (`.pdf`, `.docx`, `.md`, `.txt`) accessed more than once are automatically encrypted client-side with AES-256-GCM and cached in tenant-isolated S3 Vault.
  - Source code repositories (`.git`, `node_modules`, `vendor`) are strictly excluded.

---

## 7. Phone-First Default & Headless Execution Invariant

- **Default Target Device**: Unless the user explicitly specifies `"laptop"`, `"desktop"`, or `"computer"`, Griffin assumes all personal tasks, device controls (ringtone, wallpaper, developer options, searches), and communications target the user's primary **phone**.
- **Headless Background Execution**: Messaging and system tasks must execute headlessly in the background (using notification quick-reply or background host automation). Griffin avoids forcefully launching apps in the phone foreground to prevent interrupting user activity.
- **Immediate Push Notifications**: Any replies received on tracked job applications, cold emails, or alumni LinkedIn/WhatsApp messages must trigger an immediate high-priority push notification to the user's phone.

---

## 8. Career & Resume Quality Guardrail (Strict Claude Enforcement)

- **Resume Tailoring & Personalization**: All resume alterations, role-tailored edits, and outreach drafting against `/home/adhyan/Downloads/AJ_Resume_Context.md` must strictly be executed using **Claude Pro CLI**.
- **Prohibition of Silent Fallback**: If Claude is unavailable, rate-limited, or throws an error, Griffin **must halt and alert the user immediately**. It is strictly forbidden to fall back to Antigravity CLI or lower-tier models for resume modifications.
- **Mandatory Pre-Application Approval Gate**: Under no circumstances may Griffin auto-submit an internship application, send a LinkedIn cold message, or dispatch a cold email without explicit prior user approval. Every application package (company, role, stipend, tailored resume variant, cover note, questionnaire responses) must pause in `AWAITING_APPROVAL` (`requires_approval = true`, `draft_only = true`), send an interactive approval prompt to the user's phone, and submit only after the user explicitly reviews and confirms.


