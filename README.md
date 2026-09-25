# opendeck-nanoleaf: Nanoleaf for OpenDeck

Put your Nanoleaf light panels on Stream Deck keys with
[OpenDeck](https://github.com/nekename/OpenDeck).

- **Toggle device**: one key switches a Nanoleaf installation on or off.
- **Keys show the real state.** Switch the panels from the Nanoleaf app, a
  schedule or the controller's button and the key follows within a second.
- **Pair from the key panel**: the panel finds your device and tells you when
  to hold the button. No terminal needed.
- **Your text on the key**: the device's name, your own label, or nothing.
- **Nothing to install** besides the plugin. It is a single program with no
  runtime dependencies, for Linux, macOS and Windows.
- **Your device token stays private**: stored in your desktop keyring, never in
  plain text.

Works with devices that have Nanoleaf's local API: Light Panels, Canvas,
Shapes, Elements and Lines. Essentials bulbs and strips do not have it.

## Install

1. Download `opendeck-nanoleaf-<version>.streamDeckPlugin` from the
   [releases page](../../releases).
2. In OpenDeck, open **Settings > Plugins** and install the downloaded file.
   Alternatively unzip it into OpenDeck's plugin folder
   (`~/.config/opendeck/plugins/` on Linux) and restart OpenDeck.

## Pair with your device

Pairing is done once per device, from inside OpenDeck:

1. Drag **Toggle device** onto a key.
2. The panel below the key searches your network and lists the devices it
   finds. Pick yours and press **Continue**. If nothing is found, choose
   "Enter the address manually"; the Nanoleaf app shows the address in the
   device's settings.
3. When the panel asks, **hold the power button on the controller for 5 to 7
   seconds**, until the LEDs start flashing. The panel shows a countdown.
4. The panel switches to your device list. Done.

Your desktop may ask whether OpenDeck's plugin may use the keyring; accept it.
To add a second installation later, use **Pair another device** in the panel.

## Use the plugin

1. Drag **Toggle device** onto a key and choose the device in the panel. The
   list shows whether each device is on, off or unreachable; **Refresh**
   reloads it.
2. Press the key. It shows lit panels when the device is on and grey ones
   when it is off.

**Key text.** The panel's *Key text* setting chooses what the plugin writes on
the key: the device's name, a custom text, or none. Font, size, colour and
position are OpenDeck's own key settings.

**Something wrong?** A key flashes a warning triangle when the plugin could
not reach the device. See Troubleshooting.

## The `nanoleaf` command-line tool

Optional. Download `nanoleaf-<version>-<platform>.tar.gz` (`.zip` on Windows)
from the releases page, unpack it, and run `./nanoleaf` from that folder.

```
nanoleaf discover                   find devices on the network
nanoleaf auth                       pair (hold the power button when asked)
nanoleaf auth status                list paired devices and check their tokens
nanoleaf auth forget                revoke a device's token and remove it

nanoleaf list devices               names, state, brightness, effect (--json for scripts)
nanoleaf on|off|toggle device [name]   the default device when no name is given
nanoleaf watch                      print changes as the device reports them

nanoleaf plugin status              where the plugin is installed and logs
nanoleaf plugin debug on | off      detailed plugin log for troubleshooting
```

Names are matched without regard to case, and a unique beginning is enough.
Flags go right after the command, before the name. Add `--dry-run` to see
what would be sent without sending it. `nanoleaf --debug <command>` shows
every exchange with the device; the token is redacted.

## Troubleshooting

- **The panel finds no device.** The panels must be powered and on the same
  network. Enter the address manually if your network blocks discovery.
- **"The device did not enter pairing mode".** Hold the power button longer,
  until the controller's LEDs flash, then start again. Holding it too briefly
  only switches the panels on or off.
- **A key flashes a warning triangle.** Run `nanoleaf auth status`; the
  status column says whether the device answers. A device that was reset
  needs pairing again: `nanoleaf auth forget` then pair from the panel.
- **The key does not follow changes made elsewhere.** Restart OpenDeck. The
  plugin reconnects automatically, but a change of network can need a fresh
  start.
- **Logs.** `nanoleaf plugin status` prints the plugin's log location.
  `nanoleaf plugin debug on` followed by an OpenDeck restart records every
  exchange for a bug report; turn it off again afterwards.

## Privacy and security

- The device token is stored in your desktop keyring (GNOME Keyring, KDE
  Wallet, macOS Keychain, Windows Credential Manager). Without a keyring it
  falls back to a file readable only by your user, and tells you.
- Everything happens on your local network. Nanoleaf's local API has no
  encryption; the token is the only protection, which is why it is never
  written to logs or to OpenDeck's settings.
- `nanoleaf auth forget` revokes the token on the device itself, so a
  forgotten device holds no leftover access.

## Building from source

Go 1.26 or newer, then `make build` for the `nanoleaf` tool and
`make plugin-install` to build the plugin into OpenDeck. See
[DEVELOPMENT.md](DEVELOPMENT.md) for the layout, tests and release process.

## License

MIT, see `LICENSE`.
