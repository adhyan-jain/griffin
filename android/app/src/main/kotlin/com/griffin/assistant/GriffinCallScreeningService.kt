package com.griffin.assistant

import android.telecom.Call
import android.telecom.CallScreeningService

/**
 * Android shows an app who is calling only when the app is the phone's call-screening app. This one never
 * blocks or changes a call: it lets every call through and tells the assistant the number, so the caller can be
 * announced by name.
 */
class GriffinCallScreeningService : CallScreeningService() {
    override fun onScreenCall(details: Call.Details) {
        respondToCall(details, CallResponse.Builder().build())
        if (details.callDirection == Call.Details.DIRECTION_INCOMING) {
            (application as GriffinApp).announceIncoming(details.handle?.schemeSpecificPart)
        }
    }
}
