package com.griffin.assistant

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PhoneControlTest {
    private class Fake(val reply: String = "Alarm set for 7 AM in your Clock app.") : PhoneControlOps {
        val calls = mutableListOf<String>()
        override fun alarm(hour: Int, minute: Int, label: String, days: List<Int>): String { calls += "alarm $hour:$minute $label $days"; return reply }
        override fun timer(seconds: Int, label: String): String { calls += "timer $seconds $label"; return "Timer set for 10 minutes in your Clock app." }
        override fun openApp(name: String): String { calls += "app $name"; return reply }
        override fun openUrl(url: String): String { calls += "url $url"; return "Opening $url." }
    }

    private fun parse(json: String) = parsePhoneControl(JSONObject(json))

    @Test fun `an alarm reads hour minute label and days`() {
        val c = parse("""{"action":"set_alarm","hour":7,"minute":30,"label":"gym","days":[2,4]}""")!!
        val f = Fake()
        val r = runPhoneControl(c, f)
        assertTrue(r.ok); assertEquals(listOf("alarm 7:30 gym [2, 4]"), f.calls)
    }

    @Test fun `a timer runs for its seconds`() {
        val f = Fake(); val r = runPhoneControl(parse("""{"action":"set_timer","seconds":600}""")!!, f)
        assertTrue(r.ok); assertEquals(listOf("timer 600 "), f.calls)
    }

    @Test fun `bad alarm and timer values are refused without touching the phone`() {
        val f = Fake()
        assertFalse(runPhoneControl(parse("""{"action":"set_alarm","hour":25,"minute":0}""")!!, f).ok)
        assertFalse(runPhoneControl(parse("""{"action":"set_alarm"}""")!!, f).ok)
        assertFalse(runPhoneControl(parse("""{"action":"set_alarm","hour":7,"minute":0,"days":[9]}""")!!, f).ok)
        assertFalse(runPhoneControl(parse("""{"action":"set_timer","seconds":0}""")!!, f).ok)
        assertTrue(f.calls.isEmpty())
    }

    @Test fun `only web links open`() {
        val f = Fake()
        assertTrue(runPhoneControl(parse("""{"action":"open_url","url":"https://example.com/a"}""")!!, f).ok)
        assertFalse(runPhoneControl(parse("""{"action":"open_url","url":"intent://x#Intent;end"}""")!!, f).ok)
        assertFalse(runPhoneControl(parse("""{"action":"open_url","url":"javascript:alert(1)"}""")!!, f).ok)
        assertEquals(1, f.calls.size)
    }

    @Test fun `an app that is not found fails with the phone's words`() {
        val r = runPhoneControl(parse("""{"action":"open_app","name":"Nope"}""")!!, Fake("I could not find an app called Nope."))
        assertFalse(r.ok); assertEquals("I could not find an app called Nope.", r.text)
    }

    @Test fun `other tasks are not phone control`() {
        assertNull(parse("""{"action":"direct_call"}""")); assertNull(parsePhoneControl(null))
    }
}
