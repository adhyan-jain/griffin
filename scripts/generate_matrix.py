import os
import openpyxl
from openpyxl.styles import Font, PatternFill, Alignment, Border, Side
from openpyxl.utils import get_column_letter

wb = openpyxl.Workbook()
ws = wb.active
ws.title = "Griffin Testing Matrix"

# Color Palette
HEADER_BG = "1E1B4B"      # Deep Indigo
HEADER_FG = "FFFFFF"      # White
CATEGORY_BG = "EDE9FE"    # Soft Lavender
BORDER_COLOR = "CBD5E1"   # Light Slate
ZEBRA_BG = "F8FAFC"       # Off-white

font_title = Font(name="Calibri", size=16, bold=True, color="1E1B4B")
font_sub = Font(name="Calibri", size=11, italic=True, color="64748B")
font_header = Font(name="Calibri", size=11, bold=True, color=HEADER_FG)
font_category = Font(name="Calibri", size=11, bold=True, color="4338CA")
font_data = Font(name="Calibri", size=10)
font_bold_data = Font(name="Calibri", size=10, bold=True)

fill_header = PatternFill(start_color=HEADER_BG, end_color=HEADER_BG, fill_type="solid")
fill_cat = PatternFill(start_color=CATEGORY_BG, end_color=CATEGORY_BG, fill_type="solid")
fill_zebra = PatternFill(start_color=ZEBRA_BG, end_color=ZEBRA_BG, fill_type="solid")

thin_border = Border(
    left=Side(style='thin', color=BORDER_COLOR),
    right=Side(style='thin', color=BORDER_COLOR),
    top=Side(style='thin', color=BORDER_COLOR),
    bottom=Side(style='thin', color=BORDER_COLOR)
)

ws.merge_cells("A1:G1")
ws["A1"] = "Griffin Assistant — Comprehensive Quality & Feature Testing Matrix"
ws["A1"].font = font_title
ws["A1"].alignment = Alignment(vertical="center")

ws.merge_cells("A2:G2")
ws["A2"] = "Instructions: Mark 'T' (Passed) or 'F' (Failed) in the 'Status (T/F)' column. Note any issues or edge-case feedback in 'User Comments / Bug Notes'."
ws["A2"].font = font_sub
ws["A2"].alignment = Alignment(vertical="center")

headers = [
    "Test ID",
    "Category",
    "Test Scenario / Feature Area",
    "Exact Command / Spoken Utterance / Action",
    "Expected Behavior & Invariants",
    "Status (T / F)",
    "User Comments / Bug Notes"
]

row_num = 4
for col_num, h in enumerate(headers, 1):
    cell = ws.cell(row=row_num, column=col_num, value=h)
    cell.font = font_header
    cell.fill = fill_header
    cell.alignment = Alignment(horizontal="center" if col_num in (1, 6) else "left", vertical="center", wrap_text=True)
    cell.border = thin_border
ws.row_dimensions[row_num].height = 28

test_data = [
    # 1. Wake Phrases & Voice Activation
    ("Voice Activation", [
        ("TC-01", "Hindi Slang Wake Phrase", "Speak: 'oye bhosadike send a message to bhai saying hello'", "Assistant wakes instantly with beep, skips 'hey <name>' requirement, and queues messaging task."),
        ("TC-02", "Slang Phonetic Variations", "Speak: 'oye bhosdike what's the weather' or 'bhosadike what time is it'", "Both variations successfully wake the assistant and capture the request."),
        ("TC-03", "Standard Manager Wake Phrase", "Speak: 'hey JJ' (or your manager's configured name) followed by a command", "Wakes with tone, enters thinking phase, and executes command."),
        ("TC-04", "Griffin Global Alias Wake", "Speak: 'hey griffin what's running'", "Recognizes 'hey griffin' out of the box regardless of custom manager name."),
        ("TC-05", "Quick Settings Tile Wake", "Pull down Android notification shade and tap the 'Listen now' tile", "Triggers immediate awake listening window with bottom orb animation without voice trigger."),
        ("TC-06", "Notification 'Listen now' Action", "Tap 'Listen now' button in the persistent Griffin notification", "Immediately wakes the listening window on the phone."),
        ("TC-07", "Notification 'Stop' Action", "Tap 'Stop' button in the persistent notification", "Immediately destroys speech recognizer, clears mic usage, and removes green status dot.")
    ]),

    # 2. Audio & Microphone UI Invariants
    ("Audio & Mic UX", [
        ("TC-08", "No 'Ting' Audio Cues on Listen Loop", "Leave assistant listening in background in a quiet room for 30 seconds", "NO annoying periodic 'ting' / ping sounds every 1.5 seconds. Silence is maintained."),
        ("TC-09", "Green Privacy Dot Removal on Stop", "Tap 'Stop listening' in Griffin app or persistent notification", "Android 12-15 green microphone indicator dot disappears from the top-right corner."),
        ("TC-10", "Speech Engine Fallback Resilience", "Run app on Android device without Google offline speech pack installed", "Automatically falls back from on-device recognizer to online speech recognizer without crash loop."),
        ("TC-11", "Extended Silence Threshold", "Pause speaking mid-thought for 2-3 seconds while awake", "Assistant does not immediately cut off or time out prematurely.")
    ]),

    # 3. Voice Approvals & Draft Readout Safety
    ("Voice Approvals & Drafts", [
        ("TC-12", "Verbal Draft Readout Request", "When an approval is waiting, speak: 'read out the draft' or 'what's the draft'", "Assistant reads out the exact message/draft text aloud and keeps listening for your decision."),
        ("TC-13", "Approval Window Extension After Draft", "Listen to draft, wait 5 seconds, then say 'approve' or 'yes approve'", "Assistant successfully approves the task without requiring a second wake phrase."),
        ("TC-14", "Verbal Approval Action ('approve')", "When asked for approval, say 'approve', 'approve it', or 'yes approve'", "Griffin executes the approval token and resumes task execution."),
        ("TC-15", "Holding Task for Later Check ('ok, I'll check')", "When asked for approval, say 'ok, I'll check', 'check later', or 'leave it'", "Assistant replies 'Okay, I will hold it for you to check.' Task remains pending on dashboard."),
        ("TC-16", "Explicit Voice Rejection ('reject')", "When asked for approval, say 'no', 'cancel', or 'reject'", "Griffin marks approval as rejected and cancels outward execution safely.")
    ]),

    # 4. Task Queries & Voice Management
    ("Task Management", [
        ("TC-17", "Querying Active Open Tasks", "Speak: 'are there any open tasks?' or 'what are the open tasks?'", "Griffin queries active tasks from API and reads them out aloud."),
        ("TC-18", "Querying Task Status Summary", "Speak: 'what are my tasks' or 'list my tasks'", "Griffin reads out count of running, completed, and failed tasks."),
        ("TC-19", "Voice Cancellation of All Tasks", "Speak: 'cancel everything' or 'cancel all running tasks'", "Griffin asks for confirmation, then cancels all non-terminal tasks.")
    ]),

    # 5. Mid-Flight Task Steering & Dedup
    ("Mid-Flight Steering", [
        ("TC-20", "Mid-Flight 'Draft First' Interception", "While an outward task is executing, say: 'give me the draft first' or 'wait don't send yet'", "Ongoing task is intercepted in-place, tagged as [Draft Only], and held for your review."),
        ("TC-21", "Mid-Flight Content Additions", "While task is drafting, say: 'also mention Friday deadline'", "Steering notes appended directly to running task payload without creating a new duplicate card."),
        ("TC-22", "Thread Deduplication on Dashboard", "Send 4 consecutive follow-up queries in the same conversation thread", "All messages link to parent thread_id. Dashboard shows 1 singular thread, not 4 fragmented cards.")
    ]),

    # 6. Device Tools & App Control (Android Phone)
    ("Phone Tools & App Control", [
        ("TC-23", "Phone Media Skip / Next Song", "With Spotify/YT Music playing on phone, speak: 'next song' or 'change spotify song'", "Instantaneous skip to next song via media key without opening or switching apps."),
        ("TC-24", "Phone Media Pause & Resume", "Speak: 'pause music' then 'play music' or 'play pause'", "Immediately pauses and resumes audio playback."),
        ("TC-25", "Torch / Flashlight On & Off", "Speak: 'torch on', 'torch off', 'turn on flashlight', 'turn off flashlight'", "Instantly activates and turns off phone camera LED flash."),
        ("TC-26", "Torch Toggle & Colloquial Trigger", "Speak: 'torch' or 'toggle torch'", "Toggles flashlight state seamlessly."),
        ("TC-27", "Hindi Torch Voice Commands", "Speak: 'torch chalu karo' or 'torch band karo'", "Turns flashlight on and off with colloquial Hindi phrasing."),
        ("TC-28", "Battery Level & Charging Status", "Speak: 'battery', 'battery level', 'how much battery', or 'battery kitni hai'", "Reads out exact battery percentage and charging state aloud (e.g. 'Battery is at 82 percent and charging.')."),
        ("TC-29", "Quick Time & Date Query", "Speak: 'what time is it', 'kya time hua', or 'what is today's date'", "Immediately speaks current time or date without requiring network round-trip."),
        ("TC-30", "Camera Voice Shortcut", "Speak: 'open camera', 'camera', 'take a photo', or 'take a selfie'", "Launches phone camera app instantly in capture mode."),
        ("TC-31", "Phone Volume & Mute Control", "Speak: 'volume up', 'volume down', 'mute', or 'unmute'", "Smoothly steps media volume or silences all audio streams."),
        ("TC-32", "Voice App Launcher (English & Hindi)", "Speak: 'open Spotify', 'launch WhatsApp', 'whatsapp kholo', or 'spotify chalao'", "Directly resolves and opens the requested app."),
        ("TC-33", "Standalone App Name Triggers", "Wake assistant and say: 'calculator', 'settings', 'photos', or 'chrome'", "Opens the respective application or system settings instantly.")
    ]),

    # 7. Desktop & OS Control (Laptop Host)
    ("Desktop & OS Control", [
        ("TC-34", "Laptop Headless Media Skip (Arch/Omarchy)", "Run 'playerctl next' or trigger desktop media action from griffin-host", "Spotify track on laptop skips in under 10ms via MPRIS D-Bus."),
        ("TC-35", "Querying Laptop Spotify Playing Track", "Trigger desktop status query", "Returns accurate current artist and track name (e.g. 'August Sands - Starbloom')."),
        ("TC-36", "Host Distro & Session Auto-Detection", "Run 'make check-host' on laptop", "Correctly detects OS: linux, Distro: Arch Linux (Omarchy), Desktop: Hyprland/Wayland."),
        ("TC-37", "Multi-OS Portability (Linux/macOS/Win)", "Inspect hands/desktop driver on different OS environments", "Correctly falls back to osascript on macOS and PowerShell on Windows."),
        ("TC-38", "Headless Desktop App Launcher", "Dispatch desktop launch action for 'code' or 'spotify'", "Launches application in background without blocking host daemon.")
    ]),

    # 8. Messaging Hands & Fallbacks
    ("Messaging Hands", [
        ("TC-39", "Headless WhatsApp Web Send", "Dispatch messaging action with WhatsApp Web configured", "Sends message headlessly in background via isolated Chrome profile."),
        ("TC-40", "WhatsApp Session Persistence", "Run 'make login-whatsapp', scan QR once, restart host", "Session cookies persist in ~/.griffin-host/whatsapp-profile without re-prompting for QR code."),
        ("TC-41", "Wireless ADB Connection to Phone", "Pair phone via 'adb pair' and connect over Wi-Fi", "Device shows under 'adb devices' and griffin-host detects phone hands over Wi-Fi."),
        ("TC-42", "Locked Phone Posture Fallback", "Send message when phone screen is locked", "Host detects lock and cleanly falls back to laptop WhatsApp Web or prompts.")
    ]),

    # 9. Host Daemon & Developer Experience
    ("Host Daemon & DX", [
        ("TC-43", "Silent Background Host Daemon", "Run 'make run-host-bg' and close the VS Code terminal window", "griffin-host continues running silently as PID in ~/.griffin-host/host.pid."),
        ("TC-44", "Daemon Status Inspection", "Run 'make host-status'", "Prints daemon PID, running status, and tail of ~/.griffin-host/host.log."),
        ("TC-45", "Daemon Stop Command", "Run 'make stop-host'", "Cleanly stops background process and removes PID file."),
        ("TC-46", "Tilde Path Token Resolution", "Set GRIFFIN_HOST_TOKEN_FILE=~/.griffin-host/token in environment", "Host resolves '~' to user home directory without 'file not found' errors.")
    ]),

    # 10. Dashboard UI/UX & AX
    ("Dashboard UI/UX & AX", [
        ("TC-47", "Auto-Send Permission Policy Check", "Send routine WhatsApp or email task", "Executes without constant nagging approval prompts (policy configured for auto on user)."),
        ("TC-48", "Task State Badges & Live Status", "Inspect task cards on web dashboard while task is executing", "Visual badges transition accurately from Queued -> Running -> Completed/Draft Only."),
        ("TC-49", "Follow-Up Conversation Input", "Type or speak follow-up directly in web dashboard conversation panel", "Follow-up streams response within existing conversation context.")
    ])
]

current_row = 5
for category_name, cases in test_data:
    # Category Header Row
    ws.merge_cells(start_row=current_row, start_column=1, end_row=current_row, end_column=7)
    cat_cell = ws.cell(row=current_row, column=1, value=f"▶ CATEGORY: {category_name.upper()}")
    cat_cell.font = font_category
    cat_cell.fill = fill_cat
    cat_cell.alignment = Alignment(vertical="center", indent=1)
    for c in range(1, 8):
        ws.cell(row=current_row, column=c).border = thin_border
    ws.row_dimensions[current_row].height = 24
    current_row += 1

    for idx, (tc_id, scenario, command, expected) in enumerate(cases):
        is_zebra = (idx % 2 == 1)
        row_fill = fill_zebra if is_zebra else None

        c1 = ws.cell(row=current_row, column=1, value=tc_id)
        c2 = ws.cell(row=current_row, column=2, value=category_name)
        c3 = ws.cell(row=current_row, column=3, value=scenario)
        c4 = ws.cell(row=current_row, column=4, value=command)
        c5 = ws.cell(row=current_row, column=5, value=expected)
        c6 = ws.cell(row=current_row, column=6, value="")  # User fills T/F
        c7 = ws.cell(row=current_row, column=7, value="")  # User comments

        c1.alignment = Alignment(horizontal="center", vertical="top")
        c2.alignment = Alignment(vertical="top")
        c3.alignment = Alignment(vertical="top")
        c4.alignment = Alignment(vertical="top", wrap_text=True)
        c5.alignment = Alignment(vertical="top", wrap_text=True)
        c6.alignment = Alignment(horizontal="center", vertical="top")
        c7.alignment = Alignment(vertical="top", wrap_text=True)

        c1.font = font_bold_data
        for cell in (c2, c3, c4, c5, c6, c7):
            cell.font = font_data

        for cell in (c1, c2, c3, c4, c5, c6, c7):
            cell.border = thin_border
            if row_fill:
                cell.fill = row_fill

        ws.row_dimensions[current_row].height = 36
        current_row += 1

# Auto-fit column widths
col_widths = {
    1: 12,  # Test ID
    2: 24,  # Category
    3: 32,  # Scenario
    4: 42,  # Command / Action
    5: 50,  # Expected Result
    6: 14,  # Status (T / F)
    7: 40   # Comments / Notes
}
for col, width in col_widths.items():
    ws.column_dimensions[get_column_letter(col)].width = width

out_path_repo = "/home/adhyan/Desktop/griffin/griffin_feature_testing_matrix.xlsx"
out_path_brain = "/home/adhyan/.gemini/antigravity-cli/brain/5f398a67-6d5f-4da1-ac2b-1a7f4465363f/griffin_feature_testing_matrix.xlsx"

wb.save(out_path_repo)
wb.save(out_path_brain)
print(f"Matrix successfully generated at:\n  - {out_path_repo}\n  - {out_path_brain}")
