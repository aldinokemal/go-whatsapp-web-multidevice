export default {
    name: 'GroupInfoFromLink',
    data() {
        return {
            loading: false,
            link: '',
            groupInfo: null,
        }
    },
    methods: {
        openModal() {
            $('#modalGroupInfoFromLink').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            if (!this.link.trim()) {
                return false;
            }

            // should be a valid WhatsApp invitation URL
            try {
                const url = new URL(this.link);
                if (!url.hostname.includes('chat.whatsapp.com') || !url.pathname.includes('/')) {
                    return false;
                }
            } catch (error) {
                return false;
            }

            return true;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                let response = await this.submitApi()
                this.groupInfo = response.results;
                showSuccessInfo('Group information retrieved successfully');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.get(`/group/info-from-link`, {
                    params: {
                        link: this.link,
                    }
                })
                return response.data;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.link = '';
            this.groupInfo = null;
        },
        formatDate(dateString) {
            if (!dateString) return 'N/A';
            return moment(dateString).format('YYYY-MM-DD HH:mm');
        },
        closeModal() {
            $('#modalGroupInfoFromLink').modal('hide');
            this.handleReset();
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Group Preview</div>
            <div class="description">
                Get group information from invitation link
            </div>
        </div>
    </div>
    
    <!--  Modal Group Info From Link  -->
    <div class="ui small modal" id="modalGroupInfoFromLink">
        <i class="close icon"></i>
        <div class="header">
            Group Information Preview
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Invitation Link</label>
                    <input v-model="link" type="text"
                           placeholder="Invitation link..."
                           aria-label="Invitation Link">
                </div>
                
                <div v-if="groupInfo" class="ui segment">
                    <h4 class="ui header">Group Details</h4>
                    <div class="ui relaxed divided list">
                        <div class="item">
                            <div class="content">
                                <div class="header">Group Name</div>
                                <div class="description">{{ groupInfo.name || 'N/A' }}</div>
                            </div>
                        </div>
                        <div class="item">
                            <div class="content">
                                <div class="header">Group ID</div>
                                <div class="description">{{ groupInfo.group_id || 'N/A' }}</div>
                            </div>
                        </div>
                        <div class="item">
                            <div class="content">
                                <div class="header">Topic</div>
                                <div class="description">{{ groupInfo.topic || 'No topic set' }}</div>
                            </div>
                        </div>
                        <div class="item">
                            <div class="content">
                                <div class="header">Description</div>
                                <div class="description">{{ groupInfo.description || 'No description' }}</div>
                            </div>
                        </div>
                        <div class="item">
                            <div class="content">
                                <div class="header">Created At</div>
                                <div class="description">{{ formatDate(groupInfo.created_at) }}</div>
                            </div>
                        </div>
                        <div class="item">
                            <div class="content">
                                <div class="header">Participants</div>
                                <div class="description">{{ groupInfo.participant_count || 0 }} members</div>
                            </div>
                        </div>
                        <div class="item">
                            <div class="content">
                                <div class="header">Group Settings</div>
                                <div class="description">
                                    <div class="ui mini labels">
                                        <div class="ui label" :class="groupInfo.is_locked ? 'red' : 'green'">
                                            <i class="lock icon"></i>
                                            {{ groupInfo.is_locked ? 'Locked' : 'Unlocked' }}
                                        </div>
                                        <div class="ui label" :class="groupInfo.is_announce ? 'orange' : 'blue'">
                                            <i class="bullhorn icon"></i>
                                            {{ groupInfo.is_announce ? 'Announce Mode' : 'Regular Mode' }}
                                        </div>
                                        <div class="ui label" :class="groupInfo.is_ephemeral ? 'purple' : 'grey'">
                                            <i class="clock icon"></i>
                                            {{ groupInfo.is_ephemeral ? 'Disappearing Messages' : 'Regular Messages' }}
                                        </div>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui grey button" @click="closeModal">
                Close
            </button>
            <button class="ui approve positive right labeled icon button" 
                    :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                    @click.prevent="handleSubmit" type="button">
                Get Info
                <i class="info icon"></i>
            </button>
        </div>
    </div>
    `
}