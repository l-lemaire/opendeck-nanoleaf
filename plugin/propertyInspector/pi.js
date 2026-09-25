// Property inspector logic for the Nanoleaf plugin. The host calls
// connectOpenActionSocket with the WebSocket port and the identity of this
// inspector; we register, ask the plugin for the paired devices, and store
// the user's choice as the button's settings. While no device is paired the
// pairing wizard shows instead.
//
// Messages to the plugin:   {"event":"sendToPlugin","action","context","payload":{...}}
// Messages from the plugin: {"event":"sendToPropertyInspector","payload":{...}}
// Settings are stored with:  {"event":"setSettings","context","payload":{...}}

"use strict";

let websocket = null;
let actionInfo = null; // {action, context, payload:{settings,...}}
let settings = {};     // the button's current settings

const $ = (id) => document.getElementById(id);

// Both names are defined: OpenAction's and the legacy Elgato one, whichever
// the host calls.
function connectOpenActionSocket(port, uuid, registerEvent, info, inActionInfo) {
	actionInfo = typeof inActionInfo === "string" ? JSON.parse(inActionInfo) : inActionInfo;
	settings = (actionInfo.payload && actionInfo.payload.settings) || {};

	websocket = new WebSocket("ws://127.0.0.1:" + port);
	websocket.onopen = () => {
		websocket.send(JSON.stringify({ event: registerEvent, uuid: uuid }));
		requestTargets();
	};
	websocket.onmessage = (msg) => {
		const data = JSON.parse(msg.data);
		if (data.event === "sendToPropertyInspector") {
			onPluginMessage(data.payload || {});
		} else if (data.event === "didReceiveSettings") {
			settings = (data.payload && data.payload.settings) || {};
		}
	};
	websocket.onclose = () => setStatus("Disconnected from OpenDeck", "error");

	$("target-label").textContent = kindLabel();
	$("label").options[0].textContent = "Device name";
	$("pair-another").addEventListener("click", () => { showPairing(true); startDiscovery(); });
	$("refresh").addEventListener("click", requestTargets);
	$("target").addEventListener("change", onTargetChosen);
	$("label").addEventListener("change", onLabelChanged);
	$("custom-label").addEventListener("input", onLabelChanged);
	showLabelControls();
}
const connectElgatoStreamDeckSocket = connectOpenActionSocket;

function kindLabel() {
	return "Device";
}

function sendToPlugin(payload) {
	websocket.send(JSON.stringify({
		event: "sendToPlugin",
		action: actionInfo.action,
		context: actionInfo.context,
		payload: payload,
	}));
}

function requestTargets() {
	setStatus("Asking your devices…", "busy");
	$("target").disabled = true;
	sendToPlugin({ event: "listTargets" });
}

function onPluginMessage(payload) {
	if (payload.event === "pairing") {
		onPairingProgress(payload);
		return;
	}
	if (payload.event === "devices") {
		onDevicesFound(payload);
		return;
	}
	if (payload.event === "error") {
		if (payload.code === "not_paired") {
			showPairing(true);
			return;
		}
		setStatus(payload.message, "error");
		$("target").innerHTML = '<option value="">Unavailable</option>';
		return;
	}
	if (payload.event !== "targets") return;
	showPairing(false);

	// Target selector.
	const select = $("target");
	select.innerHTML = "";
	const placeholder = document.createElement("option");
	placeholder.value = "";
	placeholder.textContent = "— choose a device —";
	select.appendChild(placeholder);
	for (const item of payload.items) {
		const opt = document.createElement("option");
		opt.value = item.id;
		opt.textContent = item.name + (item.reachable ? (item.on ? "  (on)" : "  (off)") : "  (unreachable)");
		opt.selected = item.id === settings.device;
		select.appendChild(opt);
	}
	select.disabled = false;
	setStatus(payload.items.length + " paired device" + (payload.items.length === 1 ? "" : "s"), "ok");
}

function onTargetChosen() {
	const select = $("target");
	const opt = select.options[select.selectedIndex];
	if (!opt || !opt.value) return;
	settings.device = opt.value;
	settings.name = opt.textContent.replace(/\s+\((on|off|unreachable)\)$/, "");
	saveSettings("Saved: " + settings.name);
}

// Key text controls. The choice is stored with the other settings; the
// plugin applies it when the settings arrive.
function showLabelControls() {
	$("label").value = settings.label || "name";
	$("custom-label").value = settings.custom_label || "";
	$("custom-row").hidden = $("label").value !== "custom";
}

function onLabelChanged() {
	settings.label = $("label").value;
	settings.custom_label = $("custom-label").value;
	$("custom-row").hidden = settings.label !== "custom";
	if (settings.label !== "custom") delete settings.custom_label;
	if (!settings.device) return; // nothing to show yet; saved with the device later
	saveSettings("Saved");
}

function saveSettings(message) {
	websocket.send(JSON.stringify({ event: "setSettings", context: actionInfo.context, payload: settings }));
	setStatus(message, "ok");
}

// setStatus shows a message with a coloured dot: state is "busy", "ok",
// "error" or undefined for neutral.
function setStatus(text, state) {
	const el = $("status");
	el.textContent = text;
	el.className = "status" + (state ? " " + state : "");
}

// ---- Pairing wizard ----
// Shown while no device is paired. Discovery starts by itself; the user
// picks a device (or types an address), then is told to hold the power
// button on the controller. The plugin does the work and reports each stage.

let wizardStarted = false;
let discovered = []; // devices from the last discovery

function showPairing(show) {
	$("pair").hidden = !show;
	$("configure").hidden = show;
	if (show && !wizardStarted) {
		wizardStarted = true;
		$("pair-button").addEventListener("click", onContinue);
		$("search-again").addEventListener("click", startDiscovery);
		$("retry-button").addEventListener("click", startDiscovery);
		$("pair-address").addEventListener("input", () => { $("pair-button").disabled = !$("pair-address").value.trim(); });
		startDiscovery();
	}
}

function setStep(n) {
	for (let i = 1; i <= 3; i++) {
		const li = $("step-" + i);
		li.className = i < n ? "done" : i === n ? "current" : "";
	}
	$("pair-find").hidden = n !== 1;
	$("pair-press").hidden = n !== 2;
	$("pair-result").hidden = n !== 3;
}

function startDiscovery() {
	setStep(1);
	$("bridge-list").innerHTML = "";
	$("manual-row").hidden = true;
	$("pair-button").disabled = true;
	setStatusOn("find-status", "Searching your network for Nanoleaf devices…", "busy");
	sendToPlugin({ event: "discover" });
}

function onDevicesFound(payload) {
	discovered = payload.items || [];
	const list = $("bridge-list");
	list.innerHTML = "";

	if (discovered.length === 0) {
		setStatusOn("find-status", payload.message
			? "No device found: " + payload.message
			: "No device found on your network. Make sure the panels are powered, or enter the address below (the Nanoleaf app shows it under the device's settings).", "error");
	} else {
		setStatusOn("find-status", discovered.length === 1
			? "Found your device. Continue to pair with it."
			: "Found " + discovered.length + " devices. Choose one.", "ok");
	}

	discovered.forEach((b, i) => list.appendChild(choice("bridge", String(i),
		b.name || "Nanoleaf device", b.host + (b.model ? "  ·  " + b.model : ""), i === 0)));
	list.appendChild(choice("manual", "manual", "Enter the address manually", "", discovered.length === 0));

	onChoiceChanged();
	for (const input of list.querySelectorAll("input")) input.addEventListener("change", onChoiceChanged);
}

// choice builds one radio row.
function choice(kind, value, name, detail, checked) {
	const label = document.createElement("label");
	label.className = "choice";
	const input = document.createElement("input");
	input.type = "radio";
	input.name = "bridge-choice";
	input.value = kind + ":" + value;
	input.checked = checked;
	label.appendChild(input);
	const n = document.createElement("span");
	n.className = "name";
	n.textContent = name;
	label.appendChild(n);
	if (detail) {
		const d = document.createElement("span");
		d.className = "detail";
		d.textContent = detail;
		label.appendChild(d);
	}
	return label;
}

function selectedChoice() {
	const input = document.querySelector('input[name="bridge-choice"]:checked');
	return input ? input.value : "";
}

function onChoiceChanged() {
	const manual = selectedChoice() === "manual:manual";
	$("manual-row").hidden = !manual;
	$("pair-button").disabled = manual ? !$("pair-address").value.trim() : !selectedChoice();
	if (manual) $("pair-address").focus();
}

function onContinue() {
	const sel = selectedChoice();
	let address = "", id = "";
	if (sel === "manual:manual") {
		address = $("pair-address").value.trim();
		if (!address) return;
	} else {
		const b = discovered[Number(sel.split(":")[1])];
		if (!b) return;
		address = b.host + ":" + (b.port || 16021);
		id = b.id;
	}
	setStep(2);
	$("pair-countdown").hidden = true;
	$("press-instruction").textContent = "Contacting the device…";
	setStatusOn("press-status", "", "busy");
	sendToPlugin({ event: "pair", address: address, id: id });
}

function onPairingProgress(payload) {
	switch (payload.stage) {
		case "searching":
		case "found":
			setStep(2);
			$("press-instruction").textContent = payload.message;
			break;
		case "waiting":
			setStep(2);
			$("press-instruction").textContent = "Hold the power button on the controller for 5 to 7 seconds, until the LEDs start flashing";
			$("pair-countdown").hidden = false;
			$("pair-countdown").textContent = payload.seconds_left + " s";
			setStatusOn("press-status", "Waiting for the button…", "busy");
			break;
		case "paired":
			setStep(3);
			setStatusOn("result-status", payload.message + " Loading your devices…", "ok");
			$("retry-button").hidden = true;
			setTimeout(requestTargets, 1200);
			break;
		case "error":
			setStep(3);
			$("step-3").textContent = "Not paired";
			setStatusOn("result-status", payload.message, "error");
			$("retry-button").hidden = false;
			break;
	}
}

function setStatusOn(id, text, state) {
	const el = $(id);
	el.textContent = text;
	el.className = "status" + (state ? " " + state : "");
}
