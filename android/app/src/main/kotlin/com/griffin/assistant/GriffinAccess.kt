package com.griffin.assistant

import android.accessibilityservice.AccessibilityService
import android.content.Intent
import android.os.Bundle
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import com.griffin.assistant.core.PElement
import com.griffin.assistant.core.PScreen
import com.griffin.assistant.core.PScreenDriver
import com.griffin.assistant.core.PStep

/**
 * Lets Griffin use apps on the phone through their screens: it numbers what can be tapped or typed into, and
 * performs one step at a time. It acts only when a task the owner started is running (see [PhoneRunner]); it
 * never records or stores what it sees, never reads password fields, and only runs after the owner turns it on
 * in the phone's Accessibility settings.
 */
class GriffinAccess : AccessibilityService(), PScreenDriver {
    private var ball: FloatingBall? = null

    override fun onServiceConnected() { instance = this; syncBall() }
    override fun onUnbind(intent: Intent?): Boolean { ball?.hide(); ball = null; if (instance === this) instance = null; return super.onUnbind(intent) }

    /** Shows or hides the floating ball to match the owner's setting. */
    fun syncBall() {
        val b = ball ?: FloatingBall(this).also { ball = it }
        if (FloatingBall.enabled(this)) b.show() else b.hide()
    }
    override fun onAccessibilityEvent(event: AccessibilityEvent?) {}
    override fun onInterrupt() {}

    private class Numbered(val view: PElement, val node: AccessibilityNodeInfo)

    private fun label(n: AccessibilityNodeInfo): String =
        (n.text ?: n.contentDescription ?: n.hintText ?: n.viewIdResourceName)?.toString()?.trim()?.replace(Regex("\\s+"), " ")?.take(80).orEmpty()

    private fun roleOf(n: AccessibilityNodeInfo) = when {
        n.isEditable -> "textbox"
        n.isCheckable -> "checkbox"
        n.className?.contains("Button") == true -> "button"
        else -> "button"
    }

    /** Numbers the visible, actionable things on screen. A password field is listed but its text is never read. */
    private fun number(): Pair<PScreen, List<Numbered>>? {
        val root = rootInActiveWindow ?: return null
        val items = mutableListOf<Numbered>()
        val text = StringBuilder()
        fun walk(n: AccessibilityNodeInfo, depth: Int) {
            if (depth > 40 || items.size >= 150 || !n.isVisibleToUser) return
            if (!n.isPassword && text.length < 4000) n.text?.let { text.append(it).append('\n') }
            if (n.isClickable || n.isEditable || n.isCheckable) {
                val value = if (n.isEditable && !n.isPassword) (n.text?.toString() ?: "").take(200) else ""
                items += Numbered(PElement((items.size + 1).toString(), roleOf(n), label(n), value), n)
            }
            for (i in 0 until n.childCount) n.getChild(i)?.let { walk(it, depth + 1) }
        }
        walk(root, 0)
        val screen = PScreen(root.packageName?.toString().orEmpty(), root.window?.title?.toString().orEmpty(), text.toString(), items.map { it.view })
        return screen to items
    }

    override fun snapshot(): PScreen? = number()?.first

    override fun perform(step: PStep): String? {
        try {
            when (step.kind) {
                "open" -> return open(step.text)
                "back" -> { performGlobalAction(GLOBAL_ACTION_BACK); Thread.sleep(500); return null }
                "wait" -> { Thread.sleep(1500); return null }
                "scroll" -> {
                    val s = number()?.second?.firstNotNullOfOrNull { n -> generateSequence(n.node) { it.parent }.firstOrNull { it.isScrollable } }
                    s?.performAction(AccessibilityNodeInfo.ACTION_SCROLL_FORWARD) ?: return "nothing to scroll"
                    Thread.sleep(400); return null
                }
                "press" -> return when (step.text.lowercase()) {
                    "back" -> { performGlobalAction(GLOBAL_ACTION_BACK); null }
                    "home" -> { performGlobalAction(GLOBAL_ACTION_HOME); null }
                    "enter" -> if (android.os.Build.VERSION.SDK_INT >= 30) {
                        val f = findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
                        if (f != null && f.performAction(android.R.id.accessibilityActionImeEnter)) null else "no field is focused"
                    } else "pressing Enter needs Android 11 or later"
                    else -> "unknown key ${step.text}"
                }
                "click", "type" -> {
                    val (_, items) = number() ?: return "the screen could not be read"
                    val hit = items.firstOrNull { it.view.ref == step.ref } ?: return "element ${step.ref} is not on the screen"
                    // Refuse if the screen no longer looks like what was approved.
                    if (step.name.isNotEmpty() && (hit.view.role != step.role || hit.view.name != step.name))
                        return "the screen changed: element ${step.ref} is no longer the ${step.role} “${step.name}”"
                    if (step.kind == "click") {
                        val target = generateSequence(hit.node) { it.parent }.firstOrNull { it.isClickable } ?: hit.node
                        if (!target.performAction(AccessibilityNodeInfo.ACTION_CLICK)) return "the tap was refused"
                        Thread.sleep(900); return null
                    }
                    if (hit.node.isPassword) return "Griffin does not type into password fields"
                    hit.node.performAction(AccessibilityNodeInfo.ACTION_FOCUS)
                    val current = if (hit.node.isShowingHintText) "" else hit.node.text?.toString().orEmpty()
                    val args = Bundle().apply { putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, current + step.text) }
                    return if (hit.node.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)) null else "the field refused the text"
                }
            }
            return "unknown step ${step.kind}"
        } catch (e: Exception) {
            return "${e.javaClass.simpleName}: ${e.message}"
        }
    }

    /** Opens an app by its name or package. */
    private fun open(name: String): String? {
        val pm = packageManager
        val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val apps = pm.queryIntentActivities(launcher, 0)
        val match = apps.firstOrNull { it.activityInfo.packageName.equals(name, true) }
            ?: apps.firstOrNull { it.loadLabel(pm).toString().equals(name, true) }
            ?: apps.firstOrNull { it.loadLabel(pm).toString().contains(name, true) }
            ?: return "there is no app called “$name”"
        val intent = pm.getLaunchIntentForPackage(match.activityInfo.packageName) ?: return "that app cannot be opened"
        startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        Thread.sleep(1500)
        return null
    }

    companion object {
        @Volatile var instance: GriffinAccess? = null
        val ready: Boolean get() = instance != null
    }
}
