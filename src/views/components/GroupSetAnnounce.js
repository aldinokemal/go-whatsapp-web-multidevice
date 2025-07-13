export default {
    name: 'GroupSetAnnounce',
    data() {
        return {
            loading: false,
            groupId: '',
            announce: false,
        }
    },
    methods: {
        openModal() {
            $('#modalGroupSetAnnounce').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            return this.groupId.trim() !== '';
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalGroupSetAnnounce').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.post(`/group/announce`, {
                    group_id: this.groupId,
                    announce: this.announce
                })
                this.handleReset();
                return response.data.message;
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
            this.groupId = '';
            this.announce = false;
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Set Group Announce</div>
            <div class="description">
                Enable/disable announce mode for admins only messaging
            </div>
        </div>
    </div>
    
    <!--  Modal Group Set Announce  -->
    <div class="ui small modal" id="modalGroupSetAnnounce">
        <i class="close icon"></i>
        <div class="header">
            Set Group Announce Mode
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Group ID</label>
                    <input v-model="groupId" type="text"
                           placeholder="120363024512399999@g.us"
                           aria-label="Group ID">
                </div>
                
                <div class="field">
                    <label>Announce Mode</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" v-model="announce">
                        <label>{{ announce ? 'Enable announce mode (only admins can send messages)' : 'Disable announce mode (all members can send messages)' }}</label>
                    </div>
                    <div class="ui info message" style="margin-top: 10px;">
                        <div class="header">What does this do?</div>
                        <ul class="list">
                            <li><strong>Announce Mode ON:</strong> Only group admins can send messages to the group</li>
                            <li><strong>Announce Mode OFF:</strong> All group members can send messages</li>
                        </ul>
                    </div>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                    :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                    @click.prevent="handleSubmit" type="button">
                {{ announce ? 'Enable Announce Mode' : 'Disable Announce Mode' }}
                <i :class="announce ? 'bullhorn icon' : 'comment icon'"></i>
            </button>
        </div>
    </div>
    `
} 