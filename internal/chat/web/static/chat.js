// Chat Web UI — vanilla JS client for the ycode chat hub.
(function () {
    "use strict";

    // Detect base path (supports proxy prefix like /chat/).
    const basePath = (() => {
        const scripts = document.querySelectorAll("script[src]");
        for (const s of scripts) {
            const idx = s.src.indexOf("/chat.js");
            if (idx !== -1) {
                const url = new URL(s.src);
                return url.pathname.substring(0, idx) || "";
            }
        }
        return "";
    })();

    const apiBase = basePath + "/api";

    // State.
    let rooms = [];
    let currentRoomID = null;
    let ws = null;
    let senderName = "web-user";

    // DOM refs.
    const roomListEl = document.getElementById("room-list");
    const messagesEl = document.getElementById("messages");
    const roomNameEl = document.getElementById("room-name");
    const messageForm = document.getElementById("message-form");
    const messageInput = document.getElementById("message-input");
    const newRoomBtn = document.getElementById("new-room-btn");
    const statusDot = document.getElementById("connection-status");
    const statusText = document.getElementById("status-text");

    // --- API ---

    async function fetchJSON(url, opts) {
        const res = await fetch(url, opts);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
    }

    async function loadRooms() {
        try {
            rooms = await fetchJSON(apiBase + "/rooms");
            renderRoomList();
        } catch (e) {
            console.error("Failed to load rooms:", e);
        }
    }

    async function createRoom() {
        const name = prompt("Room name:");
        if (!name) return;
        try {
            const room = await fetchJSON(apiBase + "/rooms", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ name }),
            });
            rooms.unshift(room);
            renderRoomList();
            selectRoom(room.id);
        } catch (e) {
            console.error("Failed to create room:", e);
        }
    }

    async function loadMessages(roomID) {
        try {
            const msgs = await fetchJSON(apiBase + "/rooms/" + roomID + "/messages?limit=100");
            messagesEl.innerHTML = "";
            for (const m of msgs) {
                appendMessage(m);
            }
            scrollToBottom();
        } catch (e) {
            console.error("Failed to load messages:", e);
        }
    }

    async function sendMessage(text) {
        if (!currentRoomID || !text.trim()) return;
        try {
            await fetch(apiBase + "/rooms/" + currentRoomID + "/messages", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ text: text.trim(), sender_name: senderName }),
            });
        } catch (e) {
            console.error("Failed to send message:", e);
        }
    }

    // --- WebSocket ---

    function connectWS(roomID) {
        if (ws) {
            ws.close();
            ws = null;
        }

        const proto = location.protocol === "https:" ? "wss:" : "ws:";
        const url = proto + "//" + location.host + apiBase + "/rooms/" + roomID + "/ws";
        ws = new WebSocket(url);

        ws.onopen = () => {
            statusDot.className = "status-dot connected";
            statusText.textContent = "Connected";
        };

        ws.onclose = () => {
            statusDot.className = "status-dot disconnected";
            statusText.textContent = "Disconnected";
            // Reconnect after delay.
            setTimeout(() => {
                if (currentRoomID === roomID) connectWS(roomID);
            }, 3000);
        };

        ws.onerror = () => {
            statusDot.className = "status-dot disconnected";
            statusText.textContent = "Error";
        };

        ws.onmessage = (evt) => {
            try {
                const msg = JSON.parse(evt.data);
                if (msg.room_id === currentRoomID) {
                    appendMessage(msg);
                    scrollToBottom();
                }
            } catch (e) {
                console.error("WS parse error:", e);
            }
        };
    }

    // --- Rendering ---

    function renderRoomList() {
        roomListEl.innerHTML = "";
        for (const r of rooms) {
            const li = document.createElement("li");
            li.textContent = r.name || r.id;
            li.dataset.id = r.id;
            if (r.id === currentRoomID) li.classList.add("active");
            li.onclick = () => selectRoom(r.id);
            roomListEl.appendChild(li);
        }
    }

    function selectRoom(roomID) {
        currentRoomID = roomID;
        const room = rooms.find((r) => r.id === roomID);
        roomNameEl.textContent = room ? room.name : roomID;
        messageForm.classList.remove("hidden");
        messageInput.focus();
        renderRoomList();
        loadMessages(roomID);
        connectWS(roomID);
    }

    function appendMessage(msg) {
        const div = document.createElement("div");
        div.className = "message";

        const meta = document.createElement("div");
        meta.className = "meta";

        if (msg.origin && msg.origin.channel_id && msg.origin.channel_id !== "web") {
            const badge = document.createElement("span");
            badge.className = "channel-badge";
            badge.textContent = msg.origin.channel_id;
            meta.appendChild(badge);
        }

        const sender = document.createElement("span");
        sender.className = "sender";
        sender.textContent = msg.sender ? msg.sender.display_name || msg.sender.id : "unknown";
        meta.appendChild(sender);

        const timeEl = document.createElement("span");
        timeEl.className = "time";
        const d = new Date(msg.timestamp);
        timeEl.textContent = d.toLocaleTimeString();
        meta.appendChild(timeEl);

        const body = document.createElement("div");
        body.className = "body";
        body.textContent = msg.content ? msg.content.text : "";

        div.appendChild(meta);
        div.appendChild(body);
        messagesEl.appendChild(div);
    }

    function scrollToBottom() {
        messagesEl.scrollTop = messagesEl.scrollHeight;
    }

    // --- Events ---

    messageForm.addEventListener("submit", (e) => {
        e.preventDefault();
        const text = messageInput.value;
        messageInput.value = "";
        sendMessage(text);
    });

    newRoomBtn.addEventListener("click", createRoom);

    // --- Init ---

    loadRooms();
})();
