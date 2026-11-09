package com.griffin.assistant

import android.content.Context
import android.net.Uri
import android.provider.ContactsContract

object Contacts {
    data class Match(val name: String, val number: String)

    /** Who a phone number belongs to, or null when it is not in the contacts. */
    fun nameForNumber(context: Context, number: String): String? = try {
        val uri = Uri.withAppendedPath(ContactsContract.PhoneLookup.CONTENT_FILTER_URI, Uri.encode(number))
        context.contentResolver.query(uri, arrayOf(ContactsContract.PhoneLookup.DISPLAY_NAME), null, null, null)?.use { if (it.moveToFirst()) it.getString(0) else null }
    } catch (_: SecurityException) {
        null
    }

    /** Contacts whose name matches what was said, exact matches first. */
    fun find(context: Context, query: String): List<Match> {
        val q = query.trim()
        if (q.isEmpty()) return emptyList()
        val out = LinkedHashMap<String, Match>()
        try {
            context.contentResolver.query(
                ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
                arrayOf(ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME, ContactsContract.CommonDataKinds.Phone.NUMBER),
                "${ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME} LIKE ?", arrayOf("%$q%"), null,
            )?.use {
                while (it.moveToNext()) {
                    val m = Match(it.getString(0) ?: continue, it.getString(1) ?: continue)
                    out.putIfAbsent(m.number.filter { c -> c.isDigit() }.takeLast(10), m)
                }
            }
        } catch (_: SecurityException) {
            return emptyList()
        }
        val exact = out.values.filter { it.name.equals(q, ignoreCase = true) }
        return exact.ifEmpty { out.values.sortedBy { it.name.length } }
    }
}
