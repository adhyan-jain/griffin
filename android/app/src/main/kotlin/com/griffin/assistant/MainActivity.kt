package com.griffin.assistant

import android.Manifest
import android.app.Activity
import android.app.role.RoleManager
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.text.InputType
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.core.content.ContextCompat

/** One plain screen: sign in, allow what the assistant needs, start or stop listening, and see what it heard. */
class MainActivity : Activity() {
    private val app get() = application as GriffinApp
    private lateinit var status: TextView
    private lateinit var log: TextView
    private lateinit var email: EditText
    private lateinit var password: EditText
    private lateinit var signInButton: Button
    private lateinit var toggle: Button
    private lateinit var orb: OrbView
    private val modeListener: () -> Unit = { orb.level = app.level; if (orb.mode != app.mode) orb.mode = app.mode }

    private val permissions = arrayOf(
        Manifest.permission.RECORD_AUDIO, Manifest.permission.READ_CONTACTS, Manifest.permission.CALL_PHONE, Manifest.permission.ANSWER_PHONE_CALLS,
        Manifest.permission.READ_PHONE_STATE, Manifest.permission.SEND_SMS, Manifest.permission.POST_NOTIFICATIONS,
    )

    private fun dp(v: Int) = (v * resources.displayMetrics.density).toInt()

    private fun text(s: String, size: Float = 15f, bold: Boolean = false) = TextView(this).apply {
        this.text = s
        textSize = size
        if (bold) setTypeface(typeface, android.graphics.Typeface.BOLD)
        setPadding(0, dp(6), 0, dp(6))
    }

    private fun button(s: String, onClick: () -> Unit) = Button(this).apply {
        text = s
        isAllCaps = false
        setOnClickListener { onClick() }
        minHeight = dp(52)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val col = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(dp(20), dp(28), dp(20), dp(28)) }
        col.addView(text("Griffin", 28f, true))
        status = text("")
        col.addView(status)

        email = EditText(this).apply { hint = "Email"; inputType = InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS or InputType.TYPE_CLASS_TEXT; setSingleLine() }
        password = EditText(this).apply { hint = "Password"; inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD; setSingleLine() }
        signInButton = button("Sign in") { signInOrOut() }
        col.addView(email)
        col.addView(password)
        col.addView(signInButton)

        col.addView(text("Allow", 18f, true))
        col.addView(button("1. Microphone, contacts, calls and texts") { requestPermissions(permissions, 1) })
        col.addView(button("2. Announce who is calling") { requestScreeningRole() })
        col.addView(button("3. Keep listening in the background") { allowBackground() })

        toggle = button("Start listening") { toggleListening() }
        col.addView(button("4. Show the animation over other apps") { allowOverlay() })
        col.addView(text("Listening", 18f, true))
        col.addView(toggle)
        orb = OrbView(this)
        col.addView(orb, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, dp(120)))
        col.addView(button("Preview the animation") { previewAnimation() })

        col.addView(text("What it heard and said", 18f, true))
        log = text("", 13f).apply { setTextIsSelectable(true) }
        col.addView(log)
        setContentView(ScrollView(this).apply { addView(col); isFillViewport = true })
    }

    override fun onResume() {
        super.onResume()
        app.addModeListener(modeListener)
        modeListener()
        app.onLog = { log.text = app.logText() }
        refresh()
    }

    override fun onPause() {
        app.removeModeListener(modeListener)
        app.onLog = null
        super.onPause()
    }

    private fun granted(p: String) = ContextCompat.checkSelfPermission(this, p) == PackageManager.PERMISSION_GRANTED

    private fun refresh() {
        val missing = permissions.filter { !granted(it) }.map { it.substringAfterLast('.').lowercase().replace('_', ' ') }
        val screening = getSystemService(RoleManager::class.java).isRoleHeld(RoleManager.ROLE_CALL_SCREENING)
        val battery = getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName)
        status.text = buildString {
            append(if (app.api.signedIn) "Signed in as ${app.api.email.ifEmpty { "you" }}" else "Not signed in")
            append("\n${if (missing.isEmpty()) "Permissions: all granted" else "Missing: ${missing.joinToString(", ")}"}")
            append("\nCall announcements: ${if (screening) "on" else "off"}    Background: ${if (battery) "allowed" else "may be stopped by battery saving"}")
            append("\nAnimation over other apps: ${if (Settings.canDrawOverlays(this@MainActivity)) "on" else "off (it shows only inside this app)"}")
            append("\nWake phrase: hey ${app.api.cachedName()}. Nothing you say is kept unless it starts with that.")
        }
        signInButton.text = if (app.api.signedIn) "Sign out" else "Sign in"
        email.visibility = if (app.api.signedIn) View.GONE else View.VISIBLE
        password.visibility = email.visibility
        toggle.text = if (app.assistantRunning) "Stop listening" else "Start listening"
        log.text = app.logText()
    }

    private fun signInOrOut() {
        if (app.api.signedIn) {
            app.api.signOut()
            refresh()
            return
        }
        val e = email.text.toString().trim()
        val p = password.text.toString()
        if (e.isEmpty() || p.isEmpty()) { app.log("enter your email and password"); return }
        signInButton.isEnabled = false
        app.net.execute {
            try {
                app.api.signIn(e, p)
                app.log("signed in")
            } catch (ex: Exception) {
                app.log("sign-in failed: ${ex.message}")
            }
            app.main.post { password.text.clear(); signInButton.isEnabled = true; refresh() }
        }
    }

    private fun toggleListening() {
        if (app.assistantRunning) {
            startService(Intent(this, AssistantService::class.java).setAction(AssistantService.ACTION_STOP))
            app.main.postDelayed({ refresh() }, 400)
            return
        }
        if (!granted(Manifest.permission.RECORD_AUDIO)) { app.log("allow the microphone first (step 1)"); return }
        ContextCompat.startForegroundService(this, Intent(this, AssistantService::class.java))
        app.main.postDelayed({ refresh() }, 600)
    }

    private fun requestScreeningRole() {
        val rm = getSystemService(RoleManager::class.java)
        if (rm.isRoleHeld(RoleManager.ROLE_CALL_SCREENING)) { app.log("call announcements are already on"); return }
        startActivityForResult(rm.createRequestRoleIntent(RoleManager.ROLE_CALL_SCREENING), 2)
    }

    private fun allowOverlay() {
        if (Settings.canDrawOverlays(this)) { app.log("the animation can already show over other apps"); return }
        startActivity(Intent(Settings.ACTION_MANAGE_OVERLAY_PERMISSION, Uri.parse("package:$packageName")))
    }

    /** Shows each state for a moment, so the owner can see what listening, thinking and speaking look like. */
    private fun previewAnimation() {
        val steps = listOf(OrbView.Mode.Listening to 2500L, OrbView.Mode.Thinking to 1800L, OrbView.Mode.Speaking to 2200L, OrbView.Mode.Hidden to 0L)
        var at = 0L
        steps.forEach { (m, d) ->
            app.main.postDelayed({ if (!app.assistantRunning) { app.showMode(m); if (m == OrbView.Mode.Listening) pulseLevel() } }, at)
            at += d
        }
    }

    private fun pulseLevel() {
        for (i in 0 until 12) app.main.postDelayed({ app.showLevel(if (i % 3 == 0) 0.9f else 0.35f) }, i * 200L)
    }

    private fun allowBackground() {
        if (getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName)) { app.log("background use is already allowed"); return }
        startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName")))
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        refresh()
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        refresh()
    }
}
