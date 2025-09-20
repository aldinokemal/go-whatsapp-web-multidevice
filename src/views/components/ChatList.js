export default {
    name: 'ChatList',
    data() {
        return {
            chats: [],
            loading: false,
            searchQuery: '',
            includeMediaChats: false,
            currentPage: 1,
            pageSize: 10,
            totalChats: 0,
            selectedChatJid: ''
        }
    },
    computed: {
        totalPages() {
            return Math.ceil(this.totalChats / this.pageSize);
        },
        filteredChats() {
            if (!this.searchQuery) return this.chats;
            return this.chats.filter(chat => 
                chat.name?.toLowerCase().includes(this.searchQuery.toLowerCase()) ||
                chat.jid?.toLowerCase().includes(this.searchQuery.toLowerCase())
            );
        }
    },
    methods: {
        openModal() {
            $('#modalChatList').modal('show');
            this.loadChats();
        },
        closeModal() {
            $('#modalChatList').modal('hide');
        },
        async loadChats() {
            this.loading = true;
            try {
                const params = new URLSearchParams({
                    offset: (this.currentPage - 1) * this.pageSize,
                    limit: this.pageSize
                });
                
                if (this.searchQuery.trim()) {
                    params.append('search', this.searchQuery);
                }
                
                if (this.includeMediaChats) {
                    params.append('has_media', 'true');
                }

                const response = await window.http.get(`/chats?${params}`);
                this.chats = response.data.results?.data || [];
                this.totalChats = response.data.results?.pagination?.total || 0;
            } catch (error) {
                showErrorInfo(error.response?.data?.message || 'Failed to load chats');
            } finally {
                this.loading = false;
            }
        },
        async searchChats() {
            this.currentPage = 1;
            await this.loadChats();
        },
        nextPage() {
            if (this.currentPage < this.totalPages) {
                this.currentPage++;
                this.loadChats();
            }
        },
        prevPage() {
            if (this.currentPage > 1) {
                this.currentPage--;
                this.loadChats();
            }
        },
        selectChat(jid) {
            this.selectedChatJid = jid;
            // Store the JID for the chat messages component
            localStorage.setItem('selectedChatJid', jid);

            // Close the current modal
            $('#modalChatList').modal('hide');

            // Directly open ChatMessages modal after ChatList modal closes
            setTimeout(() => {
                // Find the ChatMessages component and call its openModal method
                if (window.ChatMessagesComponent && window.ChatMessagesComponent.openModal) {
                    window.ChatMessagesComponent.openModal();
                }
            }, 200);
        },
        formatTimestamp(timestamp) {
            if (!timestamp) return 'N/A';
            return moment(timestamp).format('MMM DD, YYYY HH:mm');
        },
        formatJid(jid) {
            if (!jid) return '';
            if (jid.includes('@g.us')) return 'Group';
            if (jid.includes('@s.whatsapp.net')) return 'Contact';
            return 'Other';
        }
    },
    mounted() {
        // Expose the component globally for other components to access
        window.ChatListComponent = this;
    },
    beforeUnmount() {
        // Clean up global reference
        if (window.ChatListComponent === this) {
            delete window.ChatListComponent;
        }
    },
    template: `
    <div class="purple card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui purple right ribbon label">Chat</a>
            <div class="header">Chat List</div>
            <div class="description">
                View all chats with search and pagination
            </div>
        </div>
    </div>

    <!--  Modal ChatList  -->
    <div class="ui large modal" id="modalChatList">
        <i class="close icon"></i>
        <div class="header">
            <i class="comments icon"></i>
            Chat List
        </div>
        <div class="content">
            <div class="ui form">
                <div class="fields">
                    <div class="twelve wide field">
                        <label>Search Chats</label>
                        <div class="ui icon input">
                            <input type="text" 
                                   placeholder="Search by name or JID..." 
                                   v-model="searchQuery"
                                   @input="searchChats">
                            <i class="search icon"></i>
                        </div>
                    </div>
                    <div class="four wide field">
                        <label>&nbsp;</label>
                        <div class="ui checkbox">
                            <input type="checkbox" v-model="includeMediaChats" @change="searchChats">
                            <label>Media chats only</label>
                        </div>
                    </div>
                </div>
            </div>
            
            <div class="ui divider"></div>
            
            <div v-if="loading" class="ui active centered inline loader"></div>
            
            <div v-else-if="filteredChats.length === 0" class="ui placeholder segment">
                <div class="ui icon header">
                    <i class="comments outline icon"></i>
                    No chats found
                </div>
            </div>
            
            <div v-else>
                <table class="ui celled striped table">
                    <thead>
                        <tr>
                            <th>Name</th>
                            <th>Type</th>
                            <th>JID</th>
                            <th>Last Message</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-for="chat in filteredChats" :key="chat.jid">
                            <td>
                                <div class="ui header">
                                    <div class="content">
                                        {{ chat.name || 'Unknown' }}
                                    </div>
                                </div>
                            </td>
                            <td>
                                <div class="ui label" :class="chat.jid?.includes('@g.us') ? 'blue' : 'green'">
                                    {{ formatJid(chat.jid) }}
                                </div>
                            </td>
                            <td class="collapsing">
                                <code>{{ chat.jid }}</code>
                            </td>
                            <td>
                                {{ formatTimestamp(chat.last_message_time) }}
                            </td>
                            <td class="collapsing">
                                <button class="ui small primary button" 
                                        @click="selectChat(chat.jid)">
                                    <i class="eye icon"></i>
                                    View Messages
                                </button>
                            </td>
                        </tr>
                    </tbody>
                </table>
                
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
    `
}