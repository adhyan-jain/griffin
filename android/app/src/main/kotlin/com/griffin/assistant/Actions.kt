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

    private var isTorchOn = false

    fun flashlight(on: Boolean? = null): String = try {
        val cm = context.getSystemService(Context.CAMERA_SERVICE) as CameraManager
        val id = cm.cameraIdList.firstOrNull { cm.getCameraCharacteristics(it).get(android.hardware.camera2.CameraCharacteristics.FLASH_INFO_AVAILABLE) == true }
        if (id == null) {
            "This phone has no flashlight."
        } else {
            val target = on ?: !isTorchOn
            cm.setTorchMode(id, target)
            isTorchOn = target
            if (target) "Flashlight on." else "Flashlight off."
        }
    } catch (e: Exception) {
        "I could not use the flashlight."
    }

    fun volume(up: Boolean): String {
        val am = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager
        repeat(3) { am.adjustStreamVolume(AudioManager.STREAM_MUSIC, if (up) AudioManager.ADJUST_RAISE else AudioManager.ADJUST_LOWER, 0) }
        return if (up) "Louder." else "Quieter."
    }

    fun mute(mute: Boolean): String = try {
        val am = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager
        val op = if (mute) AudioManager.ADJUST_MUTE else AudioManager.ADJUST_UNMUTE
        am.adjustStreamVolume(AudioManager.STREAM_MUSIC, op, 0)
        am.adjustStreamVolume(AudioManager.STREAM_RING, op, 0)
        am.adjustStreamVolume(AudioManager.STREAM_NOTIFICATION, op, 0)
        if (mute) "Muted." else "Unmuted."
    } catch (e: Exception) {
        "Could not adjust volume."
    }

    fun battery(): String = try {
        val ifilter = android.content.IntentFilter(Intent.ACTION_BATTERY_CHANGED)
        val batteryStatus: Intent? = context.registerReceiver(null, ifilter)
        val level = batteryStatus?.getIntExtra(android.os.BatteryManager.EXTRA_LEVEL, -1) ?: -1
        val scale = batteryStatus?.getIntExtra(android.os.BatteryManager.EXTRA_SCALE, -1) ?: -1
        val pct = if (level >= 0 && scale > 0) (level * 100 / scale) else -1
        val status = batteryStatus?.getIntExtra(android.os.BatteryManager.EXTRA_STATUS, -1) ?: -1
        val isCharging = status == android.os.BatteryManager.BATTERY_STATUS_CHARGING || status == android.os.BatteryManager.BATTERY_STATUS_FULL
        if (pct >= 0) {
            "Battery is at $pct percent${if (isCharging) " and charging" else ""}."
        } else {
            "Could not read battery level."
        }
    } catch (e: Exception) {
        "Could not read battery."
    }

    fun time(): String {
        val c = Calendar.getInstance()
        return "It is ${spokenTime(c.get(Calendar.HOUR_OF_DAY), c.get(Calendar.MINUTE))}."
    }

    fun date(): String {
        val sdf = java.text.SimpleDateFormat("EEEE, MMMM d", java.util.Locale.getDefault())
        return "Today is ${sdf.format(java.util.Date())}."
    }

    fun openCamera(): String = try {
        val intent = Intent(android.provider.MediaStore.INTENT_ACTION_STILL_IMAGE_CAMERA).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(intent)
        "Opening camera."
    } catch (e: Exception) {
        openApp("camera")
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
        val target = name.trim().lowercase(java.util.Locale.ROOT)
        // Common system settings shortcuts
        when (target) {
            "settings" -> return launchIntent(Intent(android.provider.Settings.ACTION_SETTINGS), "Settings")
            "wifi", "wi-fi", "wifi settings" -> return launchIntent(Intent(android.provider.Settings.ACTION_WIFI_SETTINGS), "Wi-Fi settings")
            "bluetooth", "bluetooth settings" -> return launchIntent(Intent(android.provider.Settings.ACTION_BLUETOOTH_SETTINGS), "Bluetooth settings")
            "display", "display settings" -> return launchIntent(Intent(android.provider.Settings.ACTION_DISPLAY_SETTINGS), "Display settings")
            "sound", "sounds", "sound settings" -> return launchIntent(Intent(android.provider.Settings.ACTION_SOUND_SETTINGS), "Sound settings")
            "camera" -> return openCamera()
        }

        val pm = context.packageManager
        val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val apps = pm.queryIntentActivities(launcher, 0).map { it.loadLabel(pm).toString() to it.activityInfo.packageName }

        // Find exact, prefix, substring, or package name match
        val hit = apps.firstOrNull { it.first.equals(target, true) }
            ?: apps.firstOrNull { it.first.contains(target, true) }
            ?: apps.firstOrNull { it.second.contains(target, true) }
            ?: apps.filter { it.first.contains(target, true) }.minByOrNull { it.first.length }
            ?: return "I could not find an app called $name."

        return try {
            val intent = pm.getLaunchIntentForPackage(hit.second)!!.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_RESET_TASK_IF_NEEDED)
            context.startActivity(intent)
            "Opening ${hit.first}."
        } catch (e: Exception) {
            "Android does not let me open apps while the screen is off. Open the Griffin app first, or allow display over other apps."
        }
    }

    private fun launchIntent(intent: Intent, label: String): String = try {
        context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        "Opening $label."
    } catch (e: Exception) {
        "Could not open $label."
    }

    fun openRingtoneSettings(): String = try {
        val intent = Intent(android.media.RingtoneManager.ACTION_RINGTONE_PICKER).apply {
            putExtra(android.media.RingtoneManager.EXTRA_RINGTONE_TYPE, android.media.RingtoneManager.TYPE_RINGTONE)
            putExtra(android.media.RingtoneManager.EXTRA_RINGTONE_SHOW_DEFAULT, true)
            putExtra(android.media.RingtoneManager.EXTRA_RINGTONE_SHOW_SILENT, true)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        context.startActivity(intent)
        "Opening ringtone selector."
    } catch (e: Exception) {
        launchIntent(Intent(android.provider.Settings.ACTION_SOUND_SETTINGS), "Sound and ringtone settings")
    }

    fun openWallpaperSettings(): String = try {
        val intent = Intent(Intent.ACTION_SET_WALLPAPER).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(intent)
        "Opening wallpaper settings."
    } catch (e: Exception) {
        launchIntent(Intent(android.provider.Settings.ACTION_DISPLAY_SETTINGS), "Display and wallpaper settings")
    }

    fun openDeveloperOptions(): String = try {
        val intent = Intent(android.provider.Settings.ACTION_APPLICATION_DEVELOPMENT_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(intent)
        "Opening Developer Options."
    } catch (e: Exception) {
        "Could not open Developer Options. You may need to enable developer mode in About Phone first."
    }

    fun searchWeb(query: String): String = try {
        val escaped = java.net.URLEncoder.encode(query.trim(), "UTF-8")
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse("https://www.google.com/search?q=$escaped")).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(intent)
        "Searching for $query."
    } catch (e: Exception) {
        "Could not open browser."
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
