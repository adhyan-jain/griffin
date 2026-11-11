package com.griffin.assistant

import android.app.AlarmManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.hardware.camera2.CameraManager
import android.media.AudioManager
import android.net.Uri
import android.os.Bundle
import android.telecom.TelecomManager
import android.telephony.SmsManager
import java.util.Calendar

/** Does what the owner asked on this phone. Every method says what it did, so it can be spoken back. */
class Actions(private val context: Context) {
    private val telecom get() = context.getSystemService(Context.TELECOM_SERVICE) as TelecomManager

    fun call(number: String): String? = try {
        telecom.placeCall(Uri.fromParts("tel", number, null), Bundle())
        null
    } catch (e: SecurityException) {
        "I need the permission to make phone calls."
    } catch (e: Exception) {
        "I could not place the call."
    }

    fun sendSms(number: String, message: String): String? = try {
        val sms = context.getSystemService(SmsManager::class.java)
        val parts = sms.divideMessage(message)
        if (parts.size > 1) sms.sendMultipartTextMessage(number, null, parts, null, null) else sms.sendTextMessage(number, null, message, null, null)
        null
    } catch (e: SecurityException) {
        "I need the permission to send text messages."
    } catch (e: Exception) {
        "I could not send the message."
    }

    fun answer(): String? = try {
        telecom.acceptRingingCall()
        null
    } catch (e: SecurityException) {
        "I need the permission to answer calls."
    } catch (e: Exception) {
        "There is no call to answer."
    }

    @Suppress("DEPRECATION")
    fun reject(): String? = try {
        if (telecom.endCall()) null else "There is no call to end."
    } catch (e: SecurityException) {
        "I need the permission to end calls."
    } catch (e: Exception) {
        "There is no call to end."
    }

    /** Schedules a spoken, audible alarm. Returns what to say. */
    fun alarmAt(triggerAtMillis: Long, label: String): String {
        val am = context.getSystemService(Context.ALARM_SERVICE) as AlarmManager
        val fire = PendingIntent.getBroadcast(context, (triggerAtMillis / 1000).toInt(), Intent(context, AlarmReceiver::class.java).putExtra("label", label), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val show = PendingIntent.getActivity(context, 0, Intent(context, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        am.setAlarmClock(AlarmManager.AlarmClockInfo(triggerAtMillis, show), fire) // exempt from the exact-alarm permission
        return label
    }

    fun timer(seconds: Int): String {
        alarmAt(System.currentTimeMillis() + seconds * 1000L, "Your ${spokenDuration(seconds)} timer is done.")
        return "Timer set for ${spokenDuration(seconds)}."
    }

    fun alarm(hour: Int, minute: Int): String {
        val c = Calendar.getInstance().apply { set(Calendar.HOUR_OF_DAY, hour); set(Calendar.MINUTE, minute); set(Calendar.SECOND, 0); set(Calendar.MILLISECOND, 0) }
        if (c.timeInMillis <= System.currentTimeMillis()) c.add(Calendar.DAY_OF_YEAR, 1)
        alarmAt(c.timeInMillis, "It is ${spokenTime(hour, minute)}. Your alarm.")
        return "Alarm set for ${spokenTime(hour, minute)}."
    }

    fun flashlight(on: Boolean): String = try {
        val cm = context.getSystemService(Context.CAMERA_SERVICE) as CameraManager
        val id = cm.cameraIdList.firstOrNull { cm.getCameraCharacteristics(it).get(android.hardware.camera2.CameraCharacteristics.FLASH_INFO_AVAILABLE) == true }
        if (id == null) "This phone has no flashlight." else { cm.setTorchMode(id, on); if (on) "Flashlight on." else "Flashlight off." }
    } catch (e: Exception) {
        "I could not use the flashlight."
    }

    fun volume(up: Boolean): String {
        val am = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager
        repeat(3) { am.adjustStreamVolume(AudioManager.STREAM_MUSIC, if (up) AudioManager.ADJUST_RAISE else AudioManager.ADJUST_LOWER, 0) }
        return if (up) "Louder." else "Quieter."
    }

    fun mediaNext(): String {
        dispatchMediaKey(android.view.KeyEvent.KEYCODE_MEDIA_NEXT)
        return "Skipping to next song."
    }

    fun mediaPrevious(): String {
        dispatchMediaKey(android.view.KeyEvent.KEYCODE_MEDIA_PREVIOUS)
        return "Previous song."
    }

    fun mediaPlay(): String {
        dispatchMediaKey(android.view.KeyEvent.KEYCODE_MEDIA_PLAY)
        return "Playing music."
    }

    fun mediaPause(): String {
        dispatchMediaKey(android.view.KeyEvent.KEYCODE_MEDIA_PAUSE)
        return "Paused music."
    }

    fun mediaPlayPause(): String {
        dispatchMediaKey(android.view.KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE)
        return "Toggled playback."
    }

    private fun dispatchMediaKey(keyCode: Int) {
        val am = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager
        am.dispatchMediaKeyEvent(android.view.KeyEvent(android.view.KeyEvent.ACTION_DOWN, keyCode))
        am.dispatchMediaKeyEvent(android.view.KeyEvent(android.view.KeyEvent.ACTION_UP, keyCode))
    }

    fun openApp(name: String): String {
        val pm = context.packageManager
        val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val apps = pm.queryIntentActivities(launcher, 0).map { it.loadLabel(pm).toString() to it.activityInfo.packageName }
        val hit = apps.firstOrNull { it.first.equals(name, true) } ?: apps.filter { it.first.contains(name, true) }.minByOrNull { it.first.length } ?: return "I could not find an app called $name."
        return try {
            context.startActivity(pm.getLaunchIntentForPackage(hit.second)!!.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            "Opening ${hit.first}."
        } catch (e: Exception) {
            "Android does not let me open apps while the screen is off. Open the Griffin app first, or allow display over other apps."
        }
    }

    companion object {
        fun spokenDuration(seconds: Int): String {
            val h = seconds / 3600
            val m = seconds % 3600 / 60
            val s = seconds % 60
            val parts = buildList {
                if (h > 0) add("$h ${if (h == 1) "hour" else "hours"}")
                if (m > 0) add("$m ${if (m == 1) "minute" else "minutes"}")
                if (s > 0) add("$s ${if (s == 1) "second" else "seconds"}")
            }
            return parts.joinToString(" ")
        }

        fun spokenTime(hour: Int, minute: Int): String {
            val h12 = if (hour % 12 == 0) 12 else hour % 12
            val mer = if (hour < 12) "AM" else "PM"
            return if (minute == 0) "$h12 $mer" else "$h12:${minute.toString().padStart(2, '0')} $mer"
        }
    }
}
