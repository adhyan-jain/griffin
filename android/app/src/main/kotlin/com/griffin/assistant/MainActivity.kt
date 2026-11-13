package com.griffin.assistant

import android.Manifest
import android.app.Activity
import android.app.role.RoleManager
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.text.InputType
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.core.content.ContextCompat

/**
 * One screen, three questions: is it ready, is it listening, and what do I need to do. The setup steps show
 * as a checklist that disappears once everything is allowed, so a working assistant looks like a button and an
 * animation, not a settings page.
 */
class MainActivity : Activity() {
    private val app get() = application as GriffinApp

    private object Ink {
        val bg = Color.parseColor("#0D0E13")
        val card = Color.parseColor("#171923")
        val line = Color.parseColor("#262A38")
        val text = Color.parseColor("#F2F3F7")
        val muted = Color.parseColor("#9AA0B4")
        val accent = Color.parseColor("#7C5CFF")
        val accentSoft = Color.parseColor("#2A2150")
        val good = Color.parseColor("#4ADE80")
        val warn = Color.parseColor("#FBBF24")
    }

    private lateinit var root: LinearLayout
    private lateinit var orb: OrbView
    private val modeListener: () -> Unit = { orb.level = app.level; if (orb.mode != app.mode) orb.mode = app.mode }

    private val permissions = arrayOf(
        Manifest.permission.RECORD_AUDIO, Manifest.permission.READ_CONTACTS, Manifest.permission.CALL_PHONE, Manifest.permission.ANSWER_PHONE_CALLS,
        Manifest.permission.READ_PHONE_STATE, Manifest.permission.SEND_SMS, Manifest.permission.POST_NOTIFICATIONS,
    )

    private fun dp(v: Int) = (v * resources.displayMetrics.density).toInt()

    private fun round(color: Int, radius: Int, stroke: Int? = null) = GradientDrawable().apply {
        setColor(color)
        cornerRadius = dp(radius).toFloat()
        stroke?.let { setStroke(dp(1), it) }
    }

    private fun label(s: String, size: Float, color: Int = Ink.text, bold: Boolean = false) = TextView(this).apply {
        text = s
        textSize = size
        setTextColor(color)
        if (bold) setTypeface(typeface, Typeface.BOLD)
    }

    private fun card(vararg views: View) = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        background = round(Ink.card, 20, Ink.line)
        setPadding(dp(18), dp(16), dp(18), dp(16))
        views.forEach { addView(it) }
        layoutParams = LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply { topMargin = dp(14) }
    }

    private fun pill(text: String, filled: Boolean, onClick: () -> Unit) = Button(this).apply {
        this.text = text
        isAllCaps = false
        textSize = 16f
        setTextColor(if (filled) Color.WHITE else Ink.text)
        background = round(if (filled) Ink.accent else Color.TRANSPARENT, 28, if (filled) null else Ink.line)
        stateListAnimator = null
        minHeight = dp(54)
        setOnClickListener { onClick() }
    }

    private fun small(text: String, onClick: () -> Unit) = Button(this).apply {
        this.text = text
        isAllCaps = false
        textSize = 14f
        setTextColor(Ink.text)
        background = round(Ink.accentSoft, 18)
        stateListAnimator = null
        minHeight = dp(40)
        minimumWidth = dp(84)
        setPadding(dp(14), 0, dp(14), 0)
        setOnClickListener { onClick() }
    }

    private fun input(hint: String, password: Boolean = false) = EditText(this).apply {
        this.hint = hint
        setHintTextColor(Ink.muted)
        setTextColor(Ink.text)
        textSize = 16f
        setSingleLine()
        inputType = if (password) InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD else InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS
        background = round(Color.parseColor("#10121A"), 14, Ink.line)
        setPadding(dp(14), dp(12), dp(14), dp(12))
        layoutParams = LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply { topMargin = dp(10) }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.statusBarColor = Ink.bg
        window.navigationBarColor = Ink.bg
        root = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(dp(20), dp(28), dp(20), dp(32)) }
        setContentView(ScrollView(this).apply { setBackgroundColor(Ink.bg); addView(root); isFillViewport = true })
        orb = OrbView(this)
    }

    override fun onResume() {
        super.onResume()
        app.addModeListener(modeListener)
        app.onLog = { rebuild() }
        rebuild()
        modeListener()
    }

    override fun onPause() {
        app.removeModeListener(modeListener)
        app.onLog = null
        super.onPause()
    }

    private fun granted(p: String) = ContextCompat.checkSelfPermission(this, p) == PackageManager.PERMISSION_GRANTED

    private data class Step(val title: String, val why: String, val done: Boolean, val action: String, val run: () -> Unit)

    private fun steps(): List<Step> {
        val perms = permissions.all { granted(it) }
        val screening = getSystemService(RoleManager::class.java).isRoleHeld(RoleManager.ROLE_CALL_SCREENING)
        val battery = getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName)
        val overlay = Settings.canDrawOverlays(this)
        val notifications = androidx.core.app.NotificationManagerCompat.getEnabledListenerPackages(this).contains(packageName)
        val access = GriffinAccess.ready
        return listOf(
            Step("Microphone, contacts, calls and texts", "So it can hear \"hey\", find people and act for you.", perms, "Allow") { requestPermissions(permissions, 1) },
            Step("Say who is calling", "Reads out the caller's name. It never blocks or changes a call.", screening, "Allow") { requestScreeningRole() },
            Step("Keep listening in the background", "Stops the phone's battery saver from switching it off.", battery, "Allow") { allowBackground() },
            Step("Show the listening animation", "The wave along the bottom when it wakes, over any app.", overlay, "Allow") { allowOverlay() },
            Step("Read and reply to your messages", "Lets it read chats out and reply through WhatsApp, Instagram and the rest, without opening them.", notifications, "Allow") { startActivity(Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS)) },
            Step("Let Griffin use apps for you", "Optional. Lets it tap and type in other apps for jobs you give it, and it always asks before anything is sent. Turn on \"Griffin phone control\". If it is missing or greyed out, Android is blocking apps installed outside the Play Store: open Settings > Apps > Griffin, tap the three dots, choose \"Allow restricted settings\", then come back here.", access, "Allow") { openAccessibility() },
        )
    }

    /** Opens Accessibility settings. If the service is not listed, Android's restricted-settings block is why; App info has the switch. */
    private fun openAccessibility() {
        val listed = (getSystemService(android.view.accessibility.AccessibilityManager::class.java)
            ?.installedAccessibilityServiceList ?: emptyList()).any { it.resolveInfo.serviceInfo.packageName == packageName }
        startActivity(Intent(if (listed) Settings.ACTION_ACCESSIBILITY_SETTINGS else Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
            if (!listed) data = android.net.Uri.parse("package:$packageName")
        })
    }

    private fun rebuild() {
        root.removeAllViews()
        val name = app.api.cachedName()
        val running = app.assistantRunning
        val all = steps()
        val ready = app.api.signedIn && all.all { it.done }

        // Header: the wake phrase is the product.
        root.addView(LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            addView(android.widget.ImageView(this@MainActivity).apply {
                setImageResource(R.drawable.ic_griffin_mark)
                background = round(0xFF0B0C14.toInt(), 14, Ink.line)
                scaleType = android.widget.ImageView.ScaleType.CENTER_CROP
                setPadding(dp(1), dp(1), dp(1), dp(1))
            }, LinearLayout.LayoutParams(dp(44), dp(44)))
            addView(label("Griffin", 17f, Ink.text, true).apply { setPadding(dp(12), 0, 0, 0) })
        })
        root.addView(label("Say \"hey $name\" or \"oye bhosadike\"", 24f, Ink.text, true).apply { setPadding(0, dp(4), 0, 0) })
        root.addView(label(if (running) "Listening on this phone. Nothing is kept unless it starts with that." else "Not listening right now.", 14f, Ink.muted).apply { setPadding(0, dp(6), 0, 0) })

        if (app.api.usingTestServer) {
            root.addView(card(label("Test server in use", 15f, Ink.warn, true), label("This build is talking to ${app.api.apiUrl}, not the real Griffin. Tap Sign out at the bottom to go back to the real one.", 13f, Ink.muted).apply { setPadding(0, dp(4), 0, 0) }))
        }

        // The animation stage.
        (orb.parent as? ViewGroup)?.removeView(orb)
        val stage = FrameLayoutCard(orb)
        root.addView(stage)

        if (running) {
            root.addView(pill("Listen now", filled = true) { listenNow() }.apply {
                layoutParams = LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply { topMargin = dp(16) }
            })
            root.addView(label("Same as saying \"hey $name\", without saying it. Also in the notification and as a Quick Settings tile.", 12f, Ink.muted).apply { setPadding(dp(4), dp(6), dp(4), 0) })
        }
        root.addView(pill(if (running) "Stop listening" else "Start listening", filled = !running) { toggleListening() }.apply {
            (layoutParams as? LinearLayout.LayoutParams) ?: run { layoutParams = LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT) }
            (layoutParams as LinearLayout.LayoutParams).topMargin = dp(16)
            isEnabled = app.api.signedIn
            alpha = if (isEnabled) 1f else 0.4f
        })
        root.addView(small("Preview the animation") { previewAnimation() }.apply {
            (layoutParams as? LinearLayout.LayoutParams ?: LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT)).also { it.topMargin = dp(10); it.gravity = Gravity.CENTER_HORIZONTAL; layoutParams = it }
            visibility = if (running) View.GONE else View.VISIBLE
        })

        // The floating ball needs the accessibility service, which the setup list below already asks for.
        if (GriffinAccess.ready) {
            val on = FloatingBall.enabled(this)
            root.addView(small(if (on) "Hide the floating ball" else "Show the floating ball") { FloatingBall.setEnabled(this, !on); rebuild() }.apply {
                (layoutParams as? LinearLayout.LayoutParams ?: LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT)).also { it.topMargin = dp(6); it.gravity = Gravity.CENTER_HORIZONTAL; layoutParams = it }
            })
        }

        // Account.
        if (!app.api.signedIn) {
            val email = input("Email")
            val password = input("Password", password = true)
            val go = pill("Sign in", filled = true) { signIn(email, password) }.apply { (layoutParams as? LinearLayout.LayoutParams)?.topMargin = dp(14) }
            root.addView(card(label("Sign in", 18f, Ink.text, true), label("Use the same email and password as the Griffin web app.", 13f, Ink.muted).apply { setPadding(0, dp(4), 0, 0) }, email, password, go))
        }

        // Setup: only what is still missing.
        val missing = all.filter { !it.done }
        if (missing.isNotEmpty()) {
            val rows = missing.map { step ->
                LinearLayout(this).apply {
                    orientation = LinearLayout.HORIZONTAL
                    gravity = Gravity.CENTER_VERTICAL
                    setPadding(0, dp(10), 0, dp(10))
                    val text = LinearLayout(this@MainActivity).apply {
                        orientation = LinearLayout.VERTICAL
                        addView(label(step.title, 15f, Ink.text, true))
                        addView(label(step.why, 13f, Ink.muted).apply { setPadding(0, dp(2), dp(8), 0) })
                        layoutParams = LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f)
                    }
                    addView(text)
                    addView(small(step.action) { step.run() })
                }
            }
            root.addView(card(label("Finish setting up", 18f, Ink.warn, true), *rows.toTypedArray()))
        } else if (ready) {
            root.addView(card(label("All set", 16f, Ink.good, true), label("Everything it needs is allowed.", 13f, Ink.muted).apply { setPadding(0, dp(2), 0, 0) }))
        }

        // Activity: what it did, never what anyone said.
        val lines = app.logText().lines().filter { it.isNotBlank() }.take(8)
        if (lines.isNotEmpty()) {
            root.addView(card(label("Recent activity", 14f, Ink.muted, true), label(lines.joinToString("\n"), 13f, Ink.muted).apply { setPadding(0, dp(6), 0, 0) }))
        }
        root.addView(label(if (app.api.signedIn) "Signed in as ${app.api.email.ifEmpty { "you" }}" else "Not signed in", 12f, Ink.muted).apply { setPadding(0, dp(18), 0, 0); gravity = Gravity.CENTER_HORIZONTAL })
        if (app.api.signedIn) root.addView(small("Sign out") { app.api.signOut(); rebuild() }.apply { (layoutParams as? LinearLayout.LayoutParams ?: LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT)).also { it.gravity = Gravity.CENTER_HORIZONTAL; layoutParams = it } })
    }

    /** The orb sits in a dark rounded stage. */
    private fun FrameLayoutCard(orb: OrbView): View = LinearLayout(this).apply {
        background = round(Color.parseColor("#10121A"), 24, Ink.line)
        addView(orb, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(150)))
        layoutParams = LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply { topMargin = dp(18) }
    }

    private fun signIn(email: EditText, password: EditText) {
        val e = email.text.toString().trim()
        val p = password.text.toString()
        if (e.isEmpty() || p.isEmpty()) { app.log("enter your email and password"); return }
        app.net.execute {
            try {
                app.api.signIn(e, p)
                app.log("signed in")
            } catch (ex: Exception) {
                app.log("sign-in failed: ${ex.message}")
            }
            app.main.post { rebuild() }
        }
    }

    private fun toggleListening() {
        if (app.assistantRunning) {
            startService(Intent(this, AssistantService::class.java).setAction(AssistantService.ACTION_STOP))
            app.main.postDelayed({ rebuild() }, 400)
            return
        }
        if (!granted(Manifest.permission.RECORD_AUDIO)) { app.log("allow the microphone first"); rebuild(); return }
        ContextCompat.startForegroundService(this, Intent(this, AssistantService::class.java))
        app.main.postDelayed({ rebuild() }, 600)
    }

    /** Wakes the assistant as if the wake phrase had been said. */
    private fun listenNow() {
        ContextCompat.startForegroundService(this, Intent(this, AssistantService::class.java).setAction(AssistantService.ACTION_LISTEN))
    }

    private fun requestScreeningRole() {
        val rm = getSystemService(RoleManager::class.java)
        if (rm.isRoleHeld(RoleManager.ROLE_CALL_SCREENING)) return
        startActivityForResult(rm.createRequestRoleIntent(RoleManager.ROLE_CALL_SCREENING), 2)
    }

    private fun allowBackground() {
        startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName")))
    }

    private fun allowOverlay() {
        startActivity(Intent(Settings.ACTION_MANAGE_OVERLAY_PERMISSION, Uri.parse("package:$packageName")))
    }

    /** Shows each state for a moment, so you can see what listening, thinking and speaking look like. */
    private fun previewAnimation() {
        val steps = listOf(OrbView.Mode.Listening to 2500L, OrbView.Mode.Thinking to 1800L, OrbView.Mode.Speaking to 2200L, OrbView.Mode.Hidden to 0L)
        var at = 0L
        steps.forEach { (m, d) ->
            app.main.postDelayed({ if (!app.assistantRunning) { app.showMode(m); if (m == OrbView.Mode.Listening) for (i in 0 until 12) app.main.postDelayed({ app.showLevel(if (i % 3 == 0) 0.9f else 0.35f) }, i * 200L) } }, at)
            at += d
        }
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        rebuild()
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        rebuild()
    }
}
