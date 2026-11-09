package com.griffin.assistant

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.media.AudioAttributes
import android.media.RingtoneManager
import androidx.core.app.NotificationCompat

/** Fires when a timer or alarm the assistant set goes off: a notification with the alarm sound, and a spoken line. */
class AlarmReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val label = intent.getStringExtra("label") ?: "Your alarm."
        val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val sound = RingtoneManager.getDefaultUri(RingtoneManager.TYPE_ALARM)
        nm.createNotificationChannel(
            NotificationChannel("alarms", "Timers and alarms", NotificationManager.IMPORTANCE_HIGH).apply {
                setSound(sound, AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_ALARM).build())
                enableVibration(true)
            },
        )
        nm.notify(
            label.hashCode(),
            NotificationCompat.Builder(context, "alarms").setSmallIcon(android.R.drawable.ic_lock_idle_alarm).setContentTitle("Griffin").setContentText(label)
                .setCategory(NotificationCompat.CATEGORY_ALARM).setPriority(NotificationCompat.PRIORITY_MAX).setAutoCancel(true)
                .setContentIntent(android.app.PendingIntent.getActivity(context, 0, Intent(context, MainActivity::class.java), android.app.PendingIntent.FLAG_IMMUTABLE)).build(),
        )
        (context.applicationContext as GriffinApp).speaker.say(label)
    }
}
