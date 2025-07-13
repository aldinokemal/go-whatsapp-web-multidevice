export default {
    name: 'GroupSetTopic',
    data() {
        return {
            loading: false,
            groupId: '',
            topic: '',
        }
    },
    methods: {
        openModal() {
            $('#modalGroupSetTopic').modal({
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
                $('#modalGroupSetTopic').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.post(`/group/topic`, {
                    group_id: this.groupId,
                    topic: this.topic
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
            this.topic = '';
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Set Group Topic</div>
            <div class="description">
                Set or remove group description/topic
            </div>
        </div>
    </div>
    
    <!--  Modal Group Set Topic  -->
    <div class="ui small modal" id="modalGroupSetTopic">
        <i class="close icon"></i>
        <div class="header">
            Set Group Topic
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
                    <label>Group Topic (Description)</label>
                    <textarea v-model="topic" 
                              placeholder="Enter group description/topic... Leave empty to remove topic."
                              rows="4"
                              aria-label="Group Topic">
                    </textarea>
                    <small class="text">This will be displayed as the group description. Leave empty to remove the current topic.</small>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                    :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                    @click.prevent="handleSubmit" type="button">
                {{ topic.trim() === '' ? 'Remove Topic' : 'Update Topic' }}
                <i :class="topic.trim() === '' ? 'trash icon' : 'edit icon'"></i>
            </button>
        </div>
    </div>
    `
} 