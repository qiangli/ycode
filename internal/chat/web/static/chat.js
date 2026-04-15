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
    let loadingOlder = false;
    let oldestLoaded = false;
    let messageOffset = 0;
    let typingTimer = null;

    // DOM refs.
    const roomListEl = document.getElementById("room-list");
    const messagesEl = document.getElementById("messages");
    const roomNameEl = document.getElementById("room-name");
    const messageForm = document.getElementById("message-form");
    const messageInput = document.getElementById("message-input");
    const newRoomBtn = document.getElementById("new-room-btn");
    const dashboardBtn = document.getElementById("dashboard-btn");
    const dashCloseBtn = document.getElementById("dash-close");
    const dashboardEl = document.getElementById("dashboard");
    const dashChannelsEl = document.getElementById("dash-channels");
    const dashRoomsEl = document.getElementById("dash-rooms");
    const statusDot = document.getElementById("connection-status");
    const statusText = document.getElementById("status-text");
    const typingEl = document.getElementById("typing-indicator");

    // --- API helpers ---

    async function fetchJSON(url, opts) {
        const res = await fetch(url, opts);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
    }

    // --- Rooms ---

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

    // --- Messages ---

    async function loadMessages(roomID, append) {
        try {
            const limit = 50;
            const offset = append ? messageOffset : 0;
            const msgs = await fetchJSON(
                apiBase + "/rooms/" + roomID + "/messages?limit=" + limit + "&offset=" + offset
            );
            if (!append) {
                messagesEl.innerHTML = "";
                messageOffset = 0;
            }
            if (msgs.length === 0) {
                oldestLoaded = true;
                return;
            }
            if (append) {
                // Prepend older messages.
                const scrollH = messagesEl.scrollHeight;
                for (let i = msgs.length - 1; i >= 0; i--) {
                    prependMessage(msgs[i]);
                }
                // Maintain scroll position.
                messagesEl.scrollTop = messagesEl.scrollHeight - scrollH;
            } else {
                for (const m of msgs) {
                    appendMessage(m);
                }
                scrollToBottom();
            }
            messageOffset += msgs.length;
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

    // --- Typing indicator ---

    function showTyping(name) {
        typingEl.textContent = name + " is typing...";
        typingEl.classList.remove("hidden");
        clearTimeout(typingTimer);
        typingTimer = setTimeout(() => {
            typingEl.classList.add("hidden");
        }, 3000);
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
        messageOffset = 0;
        oldestLoaded = false;
        const room = rooms.find((r) => r.id === roomID);
        roomNameEl.textContent = room ? room.name : roomID;
        messageForm.classList.remove("hidden");
        dashboardEl.classList.add("hidden");
        messageInput.focus();
        renderRoomList();
        loadMessages(roomID, false);
        connectWS(roomID);
    }

    function appendMessage(msg) {
        const div = buildMessageEl(msg);
        messagesEl.appendChild(div);
    }

    function prependMessage(msg) {
        const div = buildMessageEl(msg);
        messagesEl.insertBefore(div, messagesEl.firstChild);
    }

    function buildMessageEl(msg) {
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
        return div;
    }

    function scrollToBottom() {
        messagesEl.scrollTop = messagesEl.scrollHeight;
    }

    // --- Scroll pagination ---

    messagesEl.addEventListener("scroll", () => {
        if (messagesEl.scrollTop === 0 && !loadingOlder && !oldestLoaded && currentRoomID) {
            loadingOlder = true;
            loadMessages(currentRoomID, true).finally(() => {
                loadingOlder = false;
            });
        }
    });

    // --- Dashboard ---

    async function openDashboard() {
        dashboardEl.classList.remove("hidden");
        messageForm.classList.add("hidden");
        try {
            const data = await fetchJSON(apiBase + "/dashboard");
            renderDashboard(data);
        } catch (e) {
            console.error("Failed to load dashboard:", e);
            dashChannelsEl.innerHTML = "<p>Failed to load dashboard.</p>";
        }
    }

    function closeDashboard() {
        dashboardEl.classList.add("hidden");
        if (currentRoomID) {
            messageForm.classList.remove("hidden");
        }
    }

    function renderDashboard(data) {
        // Channels.
        dashChannelsEl.innerHTML = "";
        if (data.channels && data.channels.length > 0) {
            for (const ch of data.channels) {
                const card = document.createElement("div");
                card.className = "channel-card";

                const dot = document.createElement("span");
                dot.className = "ch-dot " + (ch.healthy ? "healthy" : "unhealthy");
                card.appendChild(dot);

                const name = document.createElement("span");
                name.className = "ch-name";
                name.textContent = ch.id;
                card.appendChild(name);

                const caps = [];
                if (ch.capabilities.threads) caps.push("threads");
                if (ch.capabilities.reactions) caps.push("reactions");
                if (ch.capabilities.media) caps.push("media");
                if (ch.capabilities.markdown) caps.push("markdown");
                if (ch.capabilities.edit_message) caps.push("edit");
                if (caps.length > 0) {
                    const capsEl = document.createElement("span");
                    capsEl.className = "ch-caps";
                    capsEl.textContent = caps.join(", ");
                    card.appendChild(capsEl);
                }

                dashChannelsEl.appendChild(card);
            }
        } else {
            dashChannelsEl.innerHTML = "<p style='color:#666'>No channels registered.</p>";
        }

        // Rooms.
        dashRoomsEl.innerHTML = "";
        if (data.rooms && data.rooms.length > 0) {
            for (const room of data.rooms) {
                const card = document.createElement("div");
                card.className = "room-card";

                // Top row: name + stats.
                const top = document.createElement("div");
                top.className = "rc-top";

                const nameEl = document.createElement("span");
                nameEl.className = "rc-name";
                nameEl.textContent = room.name || room.id;
                nameEl.onclick = () => {
                    closeDashboard();
                    selectRoom(room.id);
                };
                top.appendChild(nameEl);

                const statsEl = document.createElement("div");
                statsEl.className = "rc-stats";
                statsEl.innerHTML =
                    "<span>" + room.message_count + " msgs</span>" +
                    "<span>" + room.user_count + " users</span>" +
                    (room.last_activity
                        ? "<span>last: " + new Date(room.last_activity).toLocaleString() + "</span>"
                        : "<span>no activity</span>");
                top.appendChild(statsEl);
                card.appendChild(top);

                // Bindings row.
                if (room.bindings && room.bindings.length > 0) {
                    const bindingsEl = document.createElement("div");
                    bindingsEl.className = "rc-bindings";
                    for (const b of room.bindings) {
                        const tag = document.createElement("span");
                        tag.className = "binding-tag";
                        tag.innerHTML =
                            b.channel_id +
                            (b.account_id !== "default" ? " (" + b.account_id + ")" : "") +
                            " &rarr; " + truncate(b.chat_id, 20) +
                            ' <span class="remove-binding" data-id="' + b.id + '">&times;</span>';
                        bindingsEl.appendChild(tag);
                    }
                    card.appendChild(bindingsEl);
                }

                dashRoomsEl.appendChild(card);
            }

            // Bind remove-binding clicks.
            dashRoomsEl.querySelectorAll(".remove-binding").forEach((el) => {
                el.addEventListener("click", async (e) => {
                    e.stopPropagation();
                    const bindingId = el.dataset.id;
                    if (!confirm("Remove this binding?")) return;
                    try {
                        await fetch(apiBase + "/bindings/" + bindingId, { method: "DELETE" });
                        openDashboard(); // Refresh.
                    } catch (err) {
                        console.error("Failed to remove binding:", err);
                    }
                });
            });
        } else {
            dashRoomsEl.innerHTML = "<p style='color:#666'>No rooms yet. Create one with the + button.</p>";
        }
    }

    function truncate(s, max) {
        return s.length > max ? s.substring(0, max) + "..." : s;
    }

    // --- Events ---

    messageForm.addEventListener("submit", (e) => {
        e.preventDefault();
        const text = messageInput.value;
        messageInput.value = "";
        sendMessage(text);
    });

    newRoomBtn.addEventListener("click", createRoom);
    dashboardBtn.addEventListener("click", openDashboard);
    dashCloseBtn.addEventListener("click", closeDashboard);

    // --- Init ---

    loadRooms();
})();
