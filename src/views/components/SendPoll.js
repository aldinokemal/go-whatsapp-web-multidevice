// export Vue Component
export default {
    name: 'SendPoll',
    data() {
        return {
            poll_phone: '',
            poll_type: 'user',
            poll_loading: false,
            poll_question: '',
            poll_options: ['', ''],
            poll_max_vote: 1,
        }
    },
    computed: {
        poll_phone_id() {
            return this.poll_type === 'user' ? `${this.poll_phone}@${window.TYPEUSER}` : `${this.poll_phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        sendPollModal() {
            $('#modalSendPoll').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async sendPollProcess() {
            try {
                let response = await this.sendPollApi()
                window.showSuccessInfo(response)
                $('#modalSendPoll').modal('hide');
            } catch (err) {
                window.showErrorInfo(err)
            }
        },
        sendPollApi() {
            return new Promise(async (resolve, reject) => {
                try {
                    this.poll_loading = true;
                    const payload = {
                        phone: this.poll_phone_id,
                        question: this.poll_question,
                        max_answer: this.poll_max_vote,
                        options: this.poll_options
                    }
                    let response = await window.http.post(`/send/poll`, payload)
                    this.sendPollReset();
                    resolve(response.data.message)
                } catch (error) {
                    if (error.response) {
                        reject(error.response.data.message)
                    } else {
                        reject(error.message)
                    }
                } finally {
                    this.poll_loading = false;
                }
            })
        },
        sendPollReset() {
            this.poll_phone = '';
            this.poll_type = 'user';
            this.poll_question = '';
            this.poll_options = ['', ''];
            this.poll_max_vote = 1;
        },
        addPollOption() {
            this.poll_options.push('')
        },
        deletePollOption(index) {
            this.poll_options.splice(index, 1)
        }
    },
    template: `
    <div class="blue card" @click="sendPollModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Poll</div>
            <div class="description">
                Send a poll/vote with multiple options
            </div>
        </div>
    </div>
    
    <!--  Modal SendPoll  -->
    <div class="ui small modal" id="modalSendPoll">
        <i class="close icon"></i>
        <div class="header">
            Send Poll
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Type</label>
                    <select name="poll_type" v-model="poll_type" aria-label="type">
                        <option value="group">Group Message</option>
                        <option value="user">Private Message</option>
                    </select>
                </div>
                <div class="field">
                    <label>Phone / Group ID</label>
                    <input v-model="poll_phone" type="text" placeholder="6289..."
                           aria-label="phone">
                    <input :value="poll_phone_id" disabled aria-label="whatsapp_id">
                </div>
                <div class="field">
                    <label>Question</label>
                    <input v-model="poll_question" type="text" placeholder="Please enter question"
                           aria-label="poll question">
                </div>
                <div class="field">
                    <label>Options</label>
                    <div style="display: flex; flex-direction: column; gap: 5px">
                        <div class="ui action input" :key="index" v-for="(option, index) in poll_options">
                            <input type="text" placeholder="Option..." v-model="poll_options[index]"
                                   aria-label="poll option">
                            <button class="ui button" @click="deletePollOption(index)" type="button">
                                <i class="minus circle icon"></i>
                            </button>
                        </div>
                        <div class="field">
                            <button class="mini ui primary button" @click="addPollOption" type="button">
                                <i class="plus icon"></i> Option
                            </button>
                        </div>
                    </div>
                </div>
                <div class="field">
                    <label>Max Vote</label>
                    <input v-model="poll_max_vote" type="number" placeholder="Max Vote"
                           aria-label="poll max votes" min="0">
                </div>
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.poll_loading}"
                 @click="sendPollProcess" type="button">
                Send
                <i class="send icon"></i>
            </div>
        </div>
    </div>
`
}