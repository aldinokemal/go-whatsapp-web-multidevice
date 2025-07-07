export default {
    name: 'GroupSetLocked',
    data() {
        return {
            loading: false,
            groupId: '',
            locked: false,
        }
    },
    methods: {
        openModal() {
            $('#modalGroupSetLocked').modal({
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
                $('#modalGroupSetLocked').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.post(`/group/locked`, {
                    group_id: this.groupId,
                    locked: this.locked
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
            this.locked = false;
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Set Group Locked</div>
            <div class="description">
                Lock/unlock group info editing for admins only
            </div>
        </div>
    </div>
    
    <!--  Modal Group Set Locked  -->
    <div class="ui small modal" id="modalGroupSetLocked">
        <i class="close icon"></i>
        <div class="header">
            Set Group Locked Status
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
                    <label>Lock Status</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" v-model="locked">
                        <label>{{ locked ? 'Lock group (only admins can edit group info)' : 'Unlock group (all members can edit group info)' }}</label>
                    </div>
                    <div class="ui info message" style="margin-top: 10px;">
                        <div class="header">What does this do?</div>
                        <ul class="list">
                            <li><strong>Locked:</strong> Only group admins can change group name, description, and photo</li>
                            <li><strong>Unlocked:</strong> All group members can change group info</li>
                        </ul>
                    </div>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                    :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                    @click.prevent="handleSubmit" type="button">
                {{ locked ? 'Lock Group' : 'Unlock Group' }}
                <i :class="locked ? 'lock icon' : 'unlock icon'"></i>
            </button>
        </div>
    </div>
    `
} 