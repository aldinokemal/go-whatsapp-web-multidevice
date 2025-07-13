export default {
  name: "ChatMessages",
  data() {
    return {
      jid: "",
      messages: [],
      loading: false,
      searchQuery: "",
      startTime: "",
      endTime: "",
      isFromMe: "",
      onlyMedia: false,
      currentPage: 1,
      pageSize: 20,
      totalMessages: 0,
    };
  },
  computed: {
    totalPages() {
      return Math.ceil(this.totalMessages / this.pageSize);
    },
    formattedJid() {
      return (
        this.jid.trim() + (this.jid.includes("@") ? "" : "@s.whatsapp.net")
      );
    },
  },
  methods: {
    isValidForm() {
      return this.jid.trim().length > 0;
    },
    openModal() {
      // Check if there's a pre-selected JID from chat list
      const selectedJid = localStorage.getItem("selectedChatJid");
      if (selectedJid) {
        this.jid = selectedJid;
        localStorage.removeItem("selectedChatJid"); // Clean up

        this.loadMessages();
      }

      $("#modalChatMessages")
        .modal({
          onShow: function () {
            // Initialize accordion after modal is shown
            setTimeout(() => {
              $("#modalChatMessages .ui.accordion").accordion();
            }, 100);
          },
        })
        .modal("show");
    },
    async loadMessages() {
      if (!this.isValidForm()) {
        showErrorInfo("Please enter a valid JID");
        return;
      }

      this.loading = true;
      try {
        const params = new URLSearchParams({
          offset: (this.currentPage - 1) * this.pageSize,
          limit: this.pageSize,
        });

        if (this.searchQuery.trim()) {
          params.append("search", this.searchQuery);
        }

        if (this.startTime) {
          params.append("start_time", this.startTime);
        }

        if (this.endTime) {
          params.append("end_time", this.endTime);
        }

        if (this.isFromMe !== "") {
          params.append("is_from_me", this.isFromMe);
        }

        if (this.onlyMedia) {
          params.append("media_only", "true");
        }

        const response = await window.http.get(
          `/chat/${this.formattedJid}/messages?${params}`
        );
        this.messages = response.data.results?.data || [];
        this.totalMessages = response.data.results?.pagination?.total || 0;

        if (this.messages.length === 0) {
          showErrorInfo("No messages found for the specified criteria");
        }
      } catch (error) {
        showErrorInfo(
          error.response?.data?.message || "Failed to load messages"
        );
      } finally {
        this.loading = false;
      }
    },
    searchMessages() {
      this.currentPage = 1;
      this.loadMessages();
    },
    nextPage() {
      if (this.currentPage < this.totalPages) {
        this.currentPage++;
        this.loadMessages();
      }
    },
    prevPage() {
      if (this.currentPage > 1) {
        this.currentPage--;
        this.loadMessages();
      }
    },
    handleReset() {
      this.jid = "";
      this.messages = [];
      this.searchQuery = "";
      this.startTime = "";
      this.endTime = "";
      this.isFromMe = "";
      this.onlyMedia = false;
      this.currentPage = 1;
      this.totalMessages = 0;
    },
    formatTimestamp(timestamp) {
      if (!timestamp) return "N/A";
      return moment(timestamp).format("MMM DD, YYYY HH:mm:ss");
    },
    formatMessageType(message) {
      if (message.media_type) return message.media_type.toUpperCase();
      if (message.message_type) return message.message_type.toUpperCase();
      return "TEXT";
    },
    formatSender(message) {
      if (message.is_from_me) return "Me";
      return message.push_name || message.sender_jid || "Unknown";
    },
    getMessageContent(message) {
      if (message.content) return message.content;
      if (message.text) return message.text;
      if (message.caption) return message.caption;
      if (message.media_type) return `[${message.media_type.toUpperCase()}]`;
      return "[No content]";
    },
    getMessageStyle(message) {
      const baseStyle = {
        padding: "1em",
        margin: "0.5em 0",
      };

      if (message.is_from_me) {
        return {
          ...baseStyle,
          borderLeft: "4px solid #2185d0",
          backgroundColor: "#f8f9fa",
        };
      } else {
        return {
          ...baseStyle,
          borderLeft: "4px solid #767676",
        };
      }
    },
  },
  mounted() {
    // Store the event handler so we can remove it later
    this.handleOpenChatMessages = () => {
      this.openModal();
    };
    
    // Listen for custom event from ChatList to open modal properly
    window.addEventListener('openChatMessages', this.handleOpenChatMessages);
  },
  beforeUnmount() {
    // Clean up event listener
    if (this.handleOpenChatMessages) {
      window.removeEventListener('openChatMessages', this.handleOpenChatMessages);
    }
  },
  template: `
    <div class="purple card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui purple right ribbon label">Chat</a>
            <div class="header">Chat Messages</div>
            <div class="description">
                View messages from specific chats with advanced filtering
            </div>
        </div>
    </div>
    
    <!--  Modal ChatMessages  -->
    <div class="ui large modal" id="modalChatMessages">
        <i class="close icon"></i>
        <div class="header">
            <i class="comment icon"></i>
            Chat Messages
        </div>
        <div class="content">
            <div class="ui form">
                <div class="field">
                    <label>Chat JID</label>
                    <input type="text" 
                           placeholder="Enter phone number or full JID (e.g. 1234567890 or group-id@g.us)" 
                           v-model="jid">
                </div>
                
                <div class="ui accordion">
                    <div class="title">
                        <i class="dropdown icon"></i>
                        Advanced Filters (Optional)
                    </div>
                    <div class="content">
                        <div class="fields">
                            <div class="eight wide field">
                                <label>Search Message Content</label>
                                <input type="text" 
                                       placeholder="Search in message text..." 
                                       v-model="searchQuery">
                            </div>
                            <div class="four wide field">
                                <label>Sender Filter</label>
                                <select class="ui dropdown" v-model="isFromMe">
                                    <option value="">All messages</option>
                                    <option value="true">My messages</option>
                                    <option value="false">Their messages</option>
                                </select>
                            </div>
                            <div class="four wide field">
                                <label>&nbsp;</label>
                                <div class="ui checkbox">
                                    <input type="checkbox" v-model="onlyMedia">
                                    <label>Media only</label>
                                </div>
                            </div>
                        </div>
                        
                        <div class="fields">
                            <div class="eight wide field">
                                <label>Start Date/Time</label>
                                <input type="datetime-local" v-model="startTime">
                            </div>
                            <div class="eight wide field">
                                <label>End Date/Time</label>
                                <input type="datetime-local" v-model="endTime">
                            </div>
                        </div>
                    </div>
                </div>
            </div>
            
            <div class="ui divider"></div>
            
            <div class="actions">
                <button class="ui primary button" 
                        :class="{'disabled': !isValidForm() || loading}"
                        @click="loadMessages">
                    <i class="search icon"></i>
                    {{ loading ? 'Loading...' : 'Load Messages' }}
                </button>
                <button class="ui button" @click="handleReset">
                    <i class="refresh icon"></i>
                    Reset
                </button>
            </div>
            
            <div v-if="loading" class="ui active centered inline loader"></div>
            
            <div v-else-if="messages.length === 0 && totalMessages === 0" class="ui placeholder segment">
                <div class="ui icon header">
                    <i class="comment outline icon"></i>
                    No messages loaded
                </div>
                <p>Enter a JID and click "Load Messages" to view chat history</p>
            </div>
            
            <div v-else-if="messages.length > 0">
                <div style="padding-top: 1em; padding-bottom: 1em;">
                    <div class="ui info message">
                        <div class="header">
                            Chat Messages for {{ formattedJid }}
                        </div>
                        <p>Showing {{ messages.length }} of {{ totalMessages }} messages</p>
                    </div>
                </div>
                
                <div class="ui divided items" style="max-height: 400px; overflow-y: auto; overflow-x: hidden; -webkit-overflow-scrolling: touch; scrollbar-width: thin;">
                    <div v-for="message in messages" :key="message.id" 
                         class="item" 
                         :style="getMessageStyle(message)">
                        <div class="content">
                            <div class="header">
                                <div class="ui horizontal label" 
                                     :class="message.is_from_me ? 'blue' : 'grey'">
                                    {{ formatSender(message) }}
                                </div>
                                <div class="ui right floated horizontal label">
                                    {{ formatMessageType(message) }}
                                </div>
                            </div>
                            <div class="meta">
                                <span>{{ formatTimestamp(message.timestamp) }}</span>
                                <span v-if="message.id" class="right floated">
                                    ID: {{ message.id }}
                                </span>
                            </div>
                            <div class="description">
                                <p>{{ getMessageContent(message) }}</p>
                                <div v-if="message.url" class="ui tiny blue label">
                                    <i class="linkify icon"></i>
                                    Media Available
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
                
                <!-- Pagination -->
                <div class="ui pagination menu" v-if="totalPages > 1">
                    <a class="icon item" @click="prevPage" :class="{ disabled: currentPage === 1 }">
                        <i class="left chevron icon"></i>
                    </a>
                    <div class="item">
                        Page {{ currentPage }} of {{ totalPages }}
                    </div>
                    <a class="icon item" @click="nextPage" :class="{ disabled: currentPage === totalPages }">
                        <i class="right chevron icon"></i>
                    </a>
                </div>
            </div>
        </div>
        <div class="actions">
            <div class="ui approve button">Close</div>
        </div>
    </div>
    `,
};
