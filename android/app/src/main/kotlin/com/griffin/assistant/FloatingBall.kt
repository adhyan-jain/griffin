package com.griffin.assistant

import android.animation.ValueAnimator
import android.content.Context
import android.content.Intent
import android.graphics.PixelFormat
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
 */
class FloatingBall(private val context: Context) {
    private val app = context.applicationContext as GriffinApp
    private val wm = context.getSystemService(Context.WINDOW_SERVICE) as WindowManager
    private val prefs = context.getSharedPreferences("griffin", Context.MODE_PRIVATE)
    private val d = context.resources.displayMetrics.density
    private val size = (76 * d).toInt()
    private var view: BallView? = null
    private lateinit var params: WindowManager.LayoutParams

    private val modeListener: () -> Unit = { view?.mode = app.mode }

    fun show() {
        if (view != null || !enabled(context)) return
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
            app.addModeListener(modeListener)
        } catch (_: Exception) {
            view = null
        }
    }

    fun hide() {
        app.removeModeListener(modeListener)
        view?.let { try { wm.removeView(it) } catch (_: Exception) {} }
        view = null
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
                MotionEvent.ACTION_DOWN -> { downX = e.rawX; downY = e.rawY; startX = params.x; startY = params.y; dragging = false; ball.lift = 1f }
                MotionEvent.ACTION_MOVE -> {
                    val dx = e.rawX - downX
                    val dy = e.rawY - downY
                    if (!dragging && (kotlin.math.abs(dx) > slop || kotlin.math.abs(dy) > slop)) dragging = true
                    if (dragging) {
                        val m = context.resources.displayMetrics
                        params.x = (startX + dx).toInt().coerceIn(-size / 3, m.widthPixels - size * 2 / 3)
                        params.y = (startY + dy).toInt().coerceIn(0, m.heightPixels - size)
                        try { wm.updateViewLayout(v, params) } catch (_: Exception) {}
                    }
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                    ball.lift = 0f
                    if (e.actionMasked == MotionEvent.ACTION_UP && !dragging) { v.performClick(); wake() } else if (dragging) settle(v)
                }
            }
            return true
        }
    }

    /** Glide to the nearest side edge, tucked a little off-screen, and remember the spot. */
    private fun settle(v: View) {
        val m = context.resources.displayMetrics
        val toRight = params.x + size / 2 > m.widthPixels / 2
        val target = if (toRight) m.widthPixels - size + (8 * d).toInt() else -(8 * d).toInt()
        ValueAnimator.ofInt(params.x, target).apply {
            duration = 220
            interpolator = DecelerateInterpolator()
            addUpdateListener { params.x = it.animatedValue as Int; try { wm.updateViewLayout(v, params) } catch (_: Exception) {} }
            doOnEnd { prefs.edit().putInt("ball_x", params.x).putInt("ball_y", params.y).apply() }
            start()
        }
    }

    private fun ValueAnimator.doOnEnd(block: () -> Unit) = addListener(object : android.animation.AnimatorListenerAdapter() { override fun onAnimationEnd(a: android.animation.Animator) = block() })

    /** The same as hearing the wake phrase (and starts the assistant if it was not running). */
    private fun wake() {
        val listen = Intent(context, AssistantService::class.java).setAction(AssistantService.ACTION_LISTEN)
        try { ContextCompat.startForegroundService(context, listen) } catch (_: Exception) { app.log("could not wake from the ball") }
    }

    companion object {
        fun enabled(context: Context) = context.getSharedPreferences("griffin", Context.MODE_PRIVATE).getBoolean("ball", true)
        fun setEnabled(context: Context, on: Boolean) {
            context.getSharedPreferences("griffin", Context.MODE_PRIVATE).edit().putBoolean("ball", on).apply()
            GriffinAccess.instance?.syncBall()
        }
    }
}
