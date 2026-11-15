package com.griffin.assistant

import android.animation.Animator
import android.animation.AnimatorListenerAdapter
import android.animation.ValueAnimator
import android.content.Context
import android.content.Intent
import android.graphics.PixelFormat
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewConfiguration
import android.view.WindowManager
import android.view.animation.DecelerateInterpolator
import androidx.core.content.ContextCompat

/**
 * A small ball that floats over every app so the owner can wake the assistant with a tap instead of the wake phrase.
 * It is drawn as an accessibility overlay, so it needs no "display over other apps" permission, only the Griffin
 * accessibility service being on. Drag it anywhere; it settles against the nearest screen edge and remembers where.
 * When resting at the edge, it half-hides into the bezel and fades translucent, sliding out when touched or active.
 */
class FloatingBall(private val context: Context) {
    private val app = context.applicationContext as GriffinApp
    private val wm = context.getSystemService(Context.WINDOW_SERVICE) as WindowManager
    private val prefs = context.getSharedPreferences("griffin", Context.MODE_PRIVATE)
    private val d = context.resources.displayMetrics.density
    private val size = (76 * d).toInt()
    private var view: BallView? = null
    private lateinit var params: WindowManager.LayoutParams

    private val mainHandler = Handler(Looper.getMainLooper())
    private val hideRunnable = Runnable { halfHide() }
    private var isHalfHidden = false
    private var hideAnimator: ValueAnimator? = null
    @Volatile private var pausedByPayment = false

    private val modeListener: () -> Unit = {
        view?.let { v ->
            v.mode = app.mode
            if (app.mode != OrbView.Mode.Hidden) {
                cancelHideTimer()
                expand(v, animate = true)
            } else {
                scheduleHalfHide()
            }
        }
    }

    fun show() {
        if (view != null || !enabled(context) || pausedByPayment) return
        val v = BallView(context)
        params = WindowManager.LayoutParams(
            size, size, WindowManager.LayoutParams.TYPE_ACCESSIBILITY_OVERLAY,
            WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE or WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS or WindowManager.LayoutParams.FLAG_LAYOUT_IN_SCREEN,
            PixelFormat.TRANSLUCENT,
        ).apply {
            gravity = Gravity.TOP or Gravity.START
            x = prefs.getInt("ball_x", context.resources.displayMetrics.widthPixels - size + (8 * d).toInt())
            y = prefs.getInt("ball_y", (context.resources.displayMetrics.heightPixels * 0.55f).toInt())
        }
        v.contentDescription = "Griffin: tap to talk"
        v.setOnTouchListener(touch)
        try {
            wm.addView(v, params)
            view = v
            v.mode = app.mode
            v.alpha = 1f
            isHalfHidden = false
            app.addModeListener(modeListener)
            scheduleHalfHide()
        } catch (_: Exception) {
            view = null
        }
    }

    fun hide() {
        cancelHideTimer()
        hideAnimator?.cancel()
        hideAnimator = null
        isHalfHidden = false
        app.removeModeListener(modeListener)
        view?.let { try { wm.removeView(it) } catch (_: Exception) {} }
        view = null
    }

    /** Temporarily removes or restores the ball when opening or leaving payment/banking apps. */
    fun setPausedByPayment(paused: Boolean) {
        if (pausedByPayment == paused) return
        pausedByPayment = paused
        if (paused) {
            hide()
        } else {
            show()
        }
    }

    /** Called when settings change (e.g. toggling half-hide). */
    fun updateSettings() {
        val v = view ?: return
        if (!halfHideEnabled(context)) {
            expand(v, animate = true)
        } else {
            scheduleHalfHide()
        }
    }

    private fun isDockedOnRight(): Boolean {
        val m = context.resources.displayMetrics
        return params.x + size / 2 > m.widthPixels / 2
    }

    private fun dockedX(): Int {
        val m = context.resources.displayMetrics
        return if (isDockedOnRight()) m.widthPixels - size + (8 * d).toInt() else -(8 * d).toInt()
    }

    private fun halfHiddenX(): Int {
        val m = context.resources.displayMetrics
        return if (isDockedOnRight()) m.widthPixels - size / 2 else -(size / 2)
    }

    private fun scheduleHalfHide() {
        cancelHideTimer()
        if (!halfHideEnabled(context) || isHalfHidden || view == null || app.mode != OrbView.Mode.Hidden) return
        mainHandler.postDelayed(hideRunnable, HIDE_DELAY_MS)
    }

    private fun cancelHideTimer() {
        mainHandler.removeCallbacks(hideRunnable)
    }

    private fun halfHide() {
        val v = view ?: return
        if (!halfHideEnabled(context) || isHalfHidden || app.mode != OrbView.Mode.Hidden) return
        hideAnimator?.cancel()

        val startX = params.x
        val targetX = halfHiddenX()
        val startAlpha = v.alpha
        val targetAlpha = 0.45f

        hideAnimator = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = 280
            interpolator = DecelerateInterpolator()
            addUpdateListener { anim ->
                val f = anim.animatedFraction
                params.x = (startX + (targetX - startX) * f).toInt()
                v.alpha = startAlpha + (targetAlpha - startAlpha) * f
                try { wm.updateViewLayout(v, params) } catch (_: Exception) {}
            }
            addListener(object : AnimatorListenerAdapter() {
                override fun onAnimationEnd(animation: Animator) {
                    isHalfHidden = true
                    hideAnimator = null
                }
            })
            start()
        }
    }

    private fun expand(v: View, animate: Boolean) {
        cancelHideTimer()
        hideAnimator?.cancel()
        hideAnimator = null

        val targetX = dockedX()
        val targetAlpha = 1.0f

        if (!animate) {
            params.x = targetX
            v.alpha = targetAlpha
            isHalfHidden = false
            try { wm.updateViewLayout(v, params) } catch (_: Exception) {}
            return
        }

        if (!isHalfHidden && v.alpha >= 0.99f && params.x == targetX) return

        val startX = params.x
        val startAlpha = v.alpha

        hideAnimator = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = 200
            interpolator = DecelerateInterpolator()
            addUpdateListener { anim ->
                val f = anim.animatedFraction
                params.x = (startX + (targetX - startX) * f).toInt()
                v.alpha = startAlpha + (targetAlpha - startAlpha) * f
                try { wm.updateViewLayout(v, params) } catch (_: Exception) {}
            }
            addListener(object : AnimatorListenerAdapter() {
                override fun onAnimationEnd(animation: Animator) {
                    isHalfHidden = false
                    hideAnimator = null
                }
            })
            start()
        }
    }

    private val touch = object : View.OnTouchListener {
        private var downX = 0f
        private var downY = 0f
        private var startX = 0
        private var startY = 0
        private var dragging = false
        private val slop = ViewConfiguration.get(context).scaledTouchSlop

        override fun onTouch(v: View, e: MotionEvent): Boolean {
            val ball = v as BallView
            when (e.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    cancelHideTimer()
                    downX = e.rawX
                    downY = e.rawY
                    startX = params.x
                    startY = params.y
                    dragging = false
                    ball.lift = 1f
                }
                MotionEvent.ACTION_MOVE -> {
                    val dx = e.rawX - downX
                    val dy = e.rawY - downY
                    if (!dragging && (kotlin.math.abs(dx) > slop || kotlin.math.abs(dy) > slop)) {
                        dragging = true
                        hideAnimator?.cancel()
                        isHalfHidden = false
                        v.alpha = 1f
                    }
                    if (dragging) {
                        val m = context.resources.displayMetrics
                        params.x = (startX + dx).toInt().coerceIn(-size / 2, m.widthPixels - size / 2)
                        params.y = (startY + dy).toInt().coerceIn(0, m.heightPixels - size)
                        try { wm.updateViewLayout(v, params) } catch (_: Exception) {}
                    }
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                    ball.lift = 0f
                    if (e.actionMasked == MotionEvent.ACTION_UP && !dragging) {
                        v.performClick()
                        expand(v, animate = true)
                        wake()
                    } else if (dragging) {
                        settle(v)
                    } else {
                        scheduleHalfHide()
                    }
                }
            }
            return true
        }
    }

    /** Glide to the nearest side edge, tucked a little off-screen, and remember the spot. */
    private fun settle(v: View) {
        cancelHideTimer()
        hideAnimator?.cancel()
        val target = dockedX()
        ValueAnimator.ofInt(params.x, target).apply {
            duration = 220
            interpolator = DecelerateInterpolator()
            addUpdateListener {
                params.x = it.animatedValue as Int
                try { wm.updateViewLayout(v, params) } catch (_: Exception) {}
            }
            addListener(object : AnimatorListenerAdapter() {
                override fun onAnimationEnd(a: Animator) {
                    isHalfHidden = false
                    v.alpha = 1f
                    prefs.edit().putInt("ball_x", params.x).putInt("ball_y", params.y).apply()
                    scheduleHalfHide()
                }
            })
            start()
        }
    }

    /** The same as hearing the wake phrase (and starts the assistant if it was not running). */
    private fun wake() {
        val listen = Intent(context, AssistantService::class.java).setAction(AssistantService.ACTION_LISTEN)
        try { ContextCompat.startForegroundService(context, listen) } catch (_: Exception) { app.log("could not wake from the ball") }
    }

    companion object {
        private const val HIDE_DELAY_MS = 2500L

        fun enabled(context: Context) = context.getSharedPreferences("griffin", Context.MODE_PRIVATE).getBoolean("ball", true)
        fun setEnabled(context: Context, on: Boolean) {
            context.getSharedPreferences("griffin", Context.MODE_PRIVATE).edit().putBoolean("ball", on).apply()
            GriffinAccess.instance?.syncBall()
        }

        fun halfHideEnabled(context: Context) = context.getSharedPreferences("griffin", Context.MODE_PRIVATE).getBoolean("ball_half_hide", true)
        fun setHalfHideEnabled(context: Context, on: Boolean) {
            context.getSharedPreferences("griffin", Context.MODE_PRIVATE).edit().putBoolean("ball_half_hide", on).apply()
            GriffinAccess.instance?.syncBall()
        }

        fun hideOnPaymentApps(context: Context) = context.getSharedPreferences("griffin", Context.MODE_PRIVATE).getBoolean("ball_hide_on_payment", true)
        fun setHideOnPaymentApps(context: Context, on: Boolean) {
            context.getSharedPreferences("griffin", Context.MODE_PRIVATE).edit().putBoolean("ball_hide_on_payment", on).apply()
            GriffinAccess.instance?.syncBall()
        }
    }
}
