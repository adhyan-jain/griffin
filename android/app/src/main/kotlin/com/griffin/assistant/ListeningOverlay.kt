package com.griffin.assistant

import android.content.Context
import android.graphics.PixelFormat
import android.provider.Settings
import android.view.Gravity
import android.view.WindowManager

/**
 * Puts the listening animation on top of whatever is on screen, so the owner can see the assistant woke up
 * without opening anything. It needs the "display over other apps" permission; without it the animation shows
 * only inside the Griffin app. It cannot take touches, so it never gets in the way.
 */
class ListeningOverlay(private val context: Context) {
    private val wm = context.getSystemService(Context.WINDOW_SERVICE) as WindowManager
    private var view: OrbView? = null

    fun available() = Settings.canDrawOverlays(context)

    fun show() {
        if (view != null || !available()) return
        val v = OrbView(context)
        val params = WindowManager.LayoutParams(
            WindowManager.LayoutParams.MATCH_PARENT, (170 * context.resources.displayMetrics.density).toInt(),
            WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY,
            WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE or WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE or WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS,
            PixelFormat.TRANSLUCENT,
        ).apply { gravity = Gravity.BOTTOM }
        try {
            wm.addView(v, params)
            view = v
        } catch (_: Exception) {
            view = null
        }
    }

    fun update(mode: OrbView.Mode, level: Float) {
        view?.let { it.level = level; if (it.mode != mode) it.mode = mode }
    }

    fun hide() {
        view?.let { try { wm.removeView(it) } catch (_: Exception) {} }
        view = null
    }
}
