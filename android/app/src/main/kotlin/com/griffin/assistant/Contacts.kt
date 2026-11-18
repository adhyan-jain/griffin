package com.griffin.assistant

import android.content.Context
import android.net.Uri
import android.provider.ContactsContract

object Contacts {
    data class Match(val name: String, val number: String)

    sealed class Resolution {
        data class Resolved(val match: Match) : Resolution()
        data class Ambiguous(val query: String, val candidates: List<Match>) : Resolution()
        data class NotFound(val query: String) : Resolution()
    }

    /**
     * Resolves a contact query strictly without guessing.
     * Returns Resolved if exactly one match exists (or exact full-name match),
     * Ambiguous if multiple candidates exist without a single exact match,
     * or NotFound if zero matches exist.
     */
    fun resolve(context: Context, query: String): Resolution {
        val q = query.trim()
        if (q.isEmpty()) return Resolution.NotFound(query)
        val matches = find(context, q)
        if (matches.isEmpty()) return Resolution.NotFound(q)
        val exact = matches.filter { it.name.equals(q, ignoreCase = true) }
        if (exact.size == 1) return Resolution.Resolved(exact[0])
        if (exact.size > 1) return Resolution.Ambiguous(q, exact)
        if (matches.size == 1) return Resolution.Resolved(matches[0])
        return Resolution.Ambiguous(q, matches)
    }

    /** Who a phone number belongs to, or null when it is not in the contacts. */
    fun nameForNumber(context: Context, number: String): String? = try {
        val uri = Uri.withAppendedPath(ContactsContract.PhoneLookup.CONTENT_FILTER_URI, Uri.encode(number))
        context.contentResolver.query(uri, arrayOf(ContactsContract.PhoneLookup.DISPLAY_NAME), null, null, null)?.use { if (it.moveToFirst()) it.getString(0) else null }
    } catch (_: SecurityException) {
        null
    }

    /** Contacts whose name matches what was said, exact matches first. Supports multi-part Indian names. */
    fun find(context: Context, query: String): List<Match> {
        val q = query.trim()
        if (q.isEmpty()) return emptyList()
        val out = LinkedHashMap<String, Match>()

        fun queryDb(pattern: String) {
            try {
                context.contentResolver.query(
                    ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
                    arrayOf(ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME, ContactsContract.CommonDataKinds.Phone.NUMBER),
                    "${ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME} LIKE ?", arrayOf("%$pattern%"), null,
                )?.use {
                    while (it.moveToNext()) {
                        val m = Match(it.getString(0) ?: continue, it.getString(1) ?: continue)
                        out.putIfAbsent(m.number.filter { c -> c.isDigit() }.takeLast(10), m)
                    }
                }
            } catch (_: SecurityException) {}
        }

        queryDb(q)
        if (out.isEmpty() && q.contains(" ")) {
            for (token in q.split(Regex("\\s+"))) {
                val t = token.trim()
                if (t.length >= 3) queryDb(t)
            }
        }

        val exact = out.values.filter { it.name.equals(q, ignoreCase = true) }
        return exact.ifEmpty { out.values.sortedBy { it.name.length } }
    }

    /** Finds the WhatsApp voice call Data ID for a contact name, or returns null. Supports multi-part names. */
    fun findWhatsAppCallDataId(context: Context, query: String): Long? {
        val q = query.trim()
        if (q.isEmpty()) return null

        fun queryData(pattern: String): Long? {
            try {
                val projection = arrayOf(ContactsContract.Data._ID, ContactsContract.Data.DISPLAY_NAME)
                val selection = "${ContactsContract.Data.MIMETYPE} = ? AND ${ContactsContract.Data.DISPLAY_NAME} LIKE ?"
                val selectionArgs = arrayOf("vnd.android.cursor.item/vnd.com.whatsapp.voip.call", "%$pattern%")
                context.contentResolver.query(
                    ContactsContract.Data.CONTENT_URI,
                    projection,
                    selection,
                    selectionArgs,
                    null
                )?.use {
                    if (it.moveToFirst()) {
                        return it.getLong(0)
                    }
                }
            } catch (_: Exception) {}
            return null
        }

        val direct = queryData(q)
        if (direct != null) return direct
        if (q.contains(" ")) {
            for (token in q.split(Regex("\\s+"))) {
                val t = token.trim()
                if (t.length >= 3) {
                    val res = queryData(t)
                    if (res != null) return res
                }
            }
        }
        return null
    }
}
