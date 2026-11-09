package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Test

class CommandsTest {
    private fun p(s: String) = Commands.parse(s)

    @Test fun `calls`() {
        assertEquals(Command.Call("mom"), p("call mom"))
        assertEquals(Command.Call("mom"), p("Call my mom."))
        assertEquals(Command.Call("rahul sharma"), p("please call Rahul Sharma"))
        assertEquals(Command.Call("dad"), p("phone dad"))
        assertEquals(Command.Call("bhai"), p("ring bhai"))
    }

    @Test fun `ordinary sentences are never turned into actions`() {
        for (s in listOf("call it a day", "call me later", "I should call mom", "what is the call stack", "can you call mom", "tell me a joke",
            "open source is great", "set up my laptop", "I might set a timer later", "timer", "alarm", "text", "call")) {
            assertEquals(s, Command.None, p(s))
        }
    }

    @Test fun `texts`() {
        assertEquals(Command.Text("mom", "i'll be late"), p("text mom that I'll be late"))
        assertEquals(Command.Text("rahul", "meet at 5"), p("message Rahul saying meet at 5"))
        assertEquals(Command.Text("sam", "on my way"), p("send a text to Sam saying on my way"))
        assertEquals(Command.None, p("text mom"))            // no message: ask the brain, do not guess
        assertEquals(Command.None, p("text mom that"))
    }

    @Test fun `timers`() {
        assertEquals(Command.Timer(300), p("set a timer for 5 minutes"))
        assertEquals(Command.Timer(600), p("timer for ten minutes"))
        assertEquals(Command.Timer(3600), p("set a timer for an hour"))
        assertEquals(Command.Timer(5400), p("start a timer for 1 hour 30 minutes"))
        assertEquals(Command.Timer(90), p("set a timer for 90 seconds"))
        assertEquals(Command.Timer(1800), p("timer half an hour"))
        assertEquals(Command.Timer(900), p("set a 15 minute timer"))
        assertEquals(Command.None, p("set a timer for later"))
        assertEquals(Command.None, p("set a timer for 999 hours"))
    }

    @Test fun `alarms`() {
        assertEquals(Command.Alarm(7, 0), p("set an alarm for 7 am"))
        assertEquals(Command.Alarm(19, 30), p("set alarm at 7:30 pm"))
        assertEquals(Command.Alarm(6, 30), p("wake me at 6 30 am"))
        assertEquals(Command.Alarm(0, 15), p("set an alarm for 12:15 a.m."))
        assertEquals(Command.Alarm(12, 0), p("set an alarm for noon"))
        assertEquals(Command.Alarm(7, 0), p("wake me up at 7"))
        assertEquals(Command.None, p("set an alarm for 25:00"))
        assertEquals(Command.None, p("set an alarm for whenever"))
    }

    @Test fun `phone controls`() {
        assertEquals(Command.Flashlight(true), p("turn on the flashlight"))
        assertEquals(Command.Flashlight(false), p("flashlight off"))
        assertEquals(Command.Volume(true), p("volume up"))
        assertEquals(Command.Volume(false), p("turn the volume down"))
        assertEquals(Command.OpenApp("whatsapp"), p("open WhatsApp"))
        assertEquals(Command.OpenApp("google maps"), p("launch the Google Maps app"))
        assertEquals(Command.None, p("open a timer"))
    }

    @Test fun `call handling words`() {
        for (s in listOf("answer", "Answer it.", "pick up", "accept the call")) assertEquals(s, Command.Answer, p(s))
        for (s in listOf("reject", "decline", "hang up", "ignore it")) assertEquals(s, Command.Reject, p(s))
        for (s in listOf("stop", "be quiet", "never mind")) assertEquals(s, Command.Stop, p(s))
    }

    @Test fun `time and duration helpers refuse nonsense`() {
        assertEquals(null, Commands.parseTime("banana"))
        assertEquals(null, Commands.parseDuration("soon"))
        assertEquals(null, Commands.parseDuration("0 minutes"))
    }

    @Test fun `reading messages`() {
        assertEquals(Command.ReadMessages(null), p("read my messages"))
        assertEquals(Command.ReadMessages(null), p("read new messages"))
        assertEquals(Command.ReadMessages(null), p("check my messages"))
        assertEquals(Command.ReadMessages(null), p("do I have any messages"))
        assertEquals(Command.ReadMessages(null), p("any new messages"))
        assertEquals(Command.ReadMessages("bro"), p("read messages from bro"))
        assertEquals(Command.ReadMessages("bro"), p("what did bro say"))
        assertEquals(Command.ReadMessages("rahul"), p("read Rahul's messages"))
        assertEquals(Command.None, p("read a book"))
        assertEquals(Command.None, p("I read the messages yesterday"))
    }

    @Test fun `replying`() {
        assertEquals(Command.Reply("i'll be there in five"), p("reply saying I'll be there in five"))
        assertEquals(Command.Reply("ok", "bro"), p("reply to bro saying ok"))
        assertEquals(Command.Reply("yes", null), p("reply yes"))
        assertEquals(Command.Reply("on my way", "bro", "WhatsApp"), p("reply to bro on whatsapp saying on my way"))
        assertEquals(Command.None, p("reply"))
        assertEquals(Command.None, p("I need to reply to that email"))
    }

    @Test fun `messaging in a named app`() {
        assertEquals(Command.MessageVia("bro", "i'm late", "WhatsApp"), p("message bro on whatsapp saying I'm late"))
        assertEquals(Command.MessageVia("sam", "hello", "Instagram"), p("dm sam on insta saying hello"))
        assertEquals(Command.MessageVia("mom", "call me", "Telegram"), p("tell mom on telegram that call me"))
        // no app named: still the plain text message
        assertEquals(Command.Text("mom", "i'll be late"), p("text mom that I'll be late"))
    }
}
