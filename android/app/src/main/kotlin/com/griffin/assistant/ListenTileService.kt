package com.griffin.assistant

import android.content.Intent
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import androidx.core.content.ContextCompat

/**
 * A Quick Settings tile: pull down the shade (it works on the lock screen and over any app) and tap it to make the
 * assistant listen right now, as if you had said the wake phrase. Add it once from the tile editor.
 */
class ListenTileService : TileService() {
    override fun onStartListening() = refresh()

    override fun onClick() {
        val listen = Intent(this, AssistantService::class.java).setAction(AssistantService.ACTION_LISTEN)
        try {
            ContextCompat.startForegroundService(this, listen) // starts the assistant too if it was not running
        } catch (e: Exception) {
            (application as GriffinApp).log("could not start listening from the tile")
        }
        refresh()
    }

    private fun refresh() {
        qsTile?.apply {
            state = if ((application as GriffinApp).assistantRunning) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
            label = "Listen now"
            updateTile()
        }
    }
}
