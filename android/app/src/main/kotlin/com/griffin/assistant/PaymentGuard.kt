package com.griffin.assistant

import android.app.AppOpsManager
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.usage.UsageEvents
import android.app.usage.UsageStatsManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Handler
import android.os.Looper
import android.os.Process
import android.provider.Settings
import androidx.core.app.NotificationCompat
import com.griffin.assistant.core.PaymentPause

/**
 * Carries out [PaymentPause] on the phone. Griffin's accessibility is switched off by the service itself
 * (disableSelf); it is switched back on by writing the system's accessibility setting, which needs the
 * WRITE_SECURE_SETTINGS grant (given once over USB). Knowing which app is in front needs usage access.
 */
object PaymentGuard {
    private const val PREFS = "griffin"
    private const val KEY_PKG = "payment_pause_pkg"
    private const val KEY_SINCE = "payment_pause_since"
    private const val KEY_HOME = "payment_pause_from_home"
    private const val TICK_MS = 600L
    private const val NOTICE_ID = 4711

    private val main = Handler(Looper.getMainLooper())
    @Volatile private var state: PaymentPause.State? = null
    private var ticking = false

    fun component(ctx: Context) = ComponentName(ctx, GriffinAccess::class.java).flattenToString()

    fun canRestore(ctx: Context) = ctx.checkSelfPermission(android.Manifest.permission.WRITE_SECURE_SETTINGS) == PackageManager.PERMISSION_GRANTED

    fun canSeeFront(ctx: Context): Boolean {
        val ops = ctx.getSystemService(AppOpsManager::class.java) ?: return false
        return ops.unsafeCheckOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), ctx.packageName) == AppOpsManager.MODE_ALLOWED
    }

    /** Called by the accessibility service when a bank app comes to the front, just before it switches itself off. */
    fun stepAside(ctx: Context, pkg: String, fromHome: Boolean) {
        val app = ctx.applicationContext
        val s = PaymentPause.State(pkg, System.currentTimeMillis(), fromHome)
        state = s
        app.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString(KEY_PKG, pkg).putLong(KEY_SINCE, s.since).putBoolean(KEY_HOME, fromHome).apply()
        (app as? GriffinApp)?.log("Paused for $pkg so it can open")
        if (!canRestore(app) || !canSeeFront(app)) {
            showTurnBackOn(app, pkg)
            // Without the grants Griffin cannot watch, but it can still reopen the bank app once, fresh.
            if (fromHome) {
                state = s.copy(relaunched = true)
                main.postDelayed({ relaunch(app, pkg) }, 500L)
            }
            return
        }
        startTicking(app)
    }

    /** After a restart of the app, keep watching a pause that was in progress. */
    fun resume(ctx: Context) {
        val app = ctx.applicationContext
        val p = app.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val pkg = p.getString(KEY_PKG, null) ?: return
        state = PaymentPause.State(pkg, p.getLong(KEY_SINCE, 0L), p.getBoolean(KEY_HOME, false), relaunched = true)
        if (canRestore(app) && canSeeFront(app)) startTicking(app)
    }

    val paused: Boolean get() = state != null

    private fun startTicking(app: Context) {
        if (ticking) return
        ticking = true
        main.postDelayed(object : Runnable {
            override fun run() {
                val s = state
                if (s == null) { ticking = false; return }
                val front = frontApp(app)
                val home = homePackage(app)
                val step = PaymentPause.next(
                    s, front, System.currentTimeMillis(),
                    isPayment = PaymentApps.isPaymentApp(app, front),
                    isHome = front != null && front == home,
                    isTransient = PaymentApps.isTransientSystemPackage(front),
                )
                when (step) {
                    is PaymentPause.Step.Relaunch -> {
                        state = s.copy(relaunched = true)
                        relaunch(app, step.pkg)
                    }
                    PaymentPause.Step.Restore -> { restore(app); ticking = false; return }
                    PaymentPause.Step.Wait -> {}
                }
                main.postDelayed(this, TICK_MS)
            }
        }, 400L) // let the system see Griffin is off before the bank app is started again
    }

    private fun relaunch(app: Context, pkg: String) {
        val launch = app.packageManager.getLaunchIntentForPackage(pkg) ?: return
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK)
        try {
            app.startActivity(launch)
            (app as? GriffinApp)?.log("Reopened $pkg with Griffin off")
        } catch (e: Exception) {
            (app as? GriffinApp)?.log("Could not reopen $pkg: ${e.message}")
        }
    }

    /** Switches Griffin's accessibility back on, keeping any other services the owner has on. */
    fun restore(app: Context) {
        state = null
        app.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().remove(KEY_PKG).remove(KEY_SINCE).remove(KEY_HOME).apply()
        app.getSystemService(NotificationManager::class.java)?.cancel(NOTICE_ID)
        if (!canRestore(app)) return
        try {
            val cr = app.contentResolver
            val now = Settings.Secure.getString(cr, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES)
            Settings.Secure.putString(cr, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES, PaymentPause.with(now, component(app)))
            Settings.Secure.putInt(cr, Settings.Secure.ACCESSIBILITY_ENABLED, 1)
            (app as? GriffinApp)?.log("Griffin is back on")
        } catch (e: Exception) {
            (app as? GriffinApp)?.log("Could not turn Griffin back on: ${e.message}")
        }
    }

    /** The app most recently brought to the front, from usage events; null when unknown. */
    private fun frontApp(app: Context): String? {
        val usm = app.getSystemService(UsageStatsManager::class.java) ?: return null
        val now = System.currentTimeMillis()
        val events = usm.queryEvents(now - 60_000, now)
        val e = UsageEvents.Event()
        var last: String? = null
        while (events.hasNextEvent()) {
            events.getNextEvent(e)
            if (e.eventType == UsageEvents.Event.ACTIVITY_RESUMED) last = e.packageName
        }
        return last
    }

    fun homePackage(ctx: Context): String? =
        ctx.packageManager.resolveActivity(Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_HOME), PackageManager.MATCH_DEFAULT_ONLY)
            ?.activityInfo?.packageName

    /** Without the one-time grants Griffin cannot turn itself back on, so it asks the owner with one tap. */
    private fun showTurnBackOn(app: Context, pkg: String) {
        val open = PendingIntent.getActivity(
            app, NOTICE_ID, Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            PendingIntent.FLAG_IMMUTABLE,
        )
        val label = try { app.packageManager.getApplicationLabel(app.packageManager.getApplicationInfo(pkg, 0)).toString() } catch (_: Exception) { pkg }
        val n = NotificationCompat.Builder(app, "griffin_task_updates")
            .setSmallIcon(android.R.drawable.ic_lock_idle_lock)
            .setContentTitle("Griffin paused for $label")
            .setContentText("Tap to turn Griffin back on when you are done.")
            .setContentIntent(open)
            .setAutoCancel(true)
            .build()
        app.getSystemService(NotificationManager::class.java)?.notify(NOTICE_ID, n)
    }
}
