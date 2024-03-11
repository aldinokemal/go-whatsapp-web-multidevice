export default {
    name: 'Send',
    data() {
        return {
            phone: '',
            type: 'user',
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.type === 'user' ? `${this.phone}@${window.TYPEUSER}` : `${this.phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        sendModal() {
            $('#modalAudioSend').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async sendProcess() {
            try {
                let response = await this.sendApi()
                showSuccessInfo(response)
                $('#modalAudioSend').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        sendApi() {
            return new Promise(async (resolve, reject) => {
                try {
                    this.loading = true;
                    let payload = new FormData();
                    payload.append("phone", this.phone_id)
                    payload.append("audio", $("#file")[0].files[0])
                    let response = await http.post(`/send/audio`, payload)
                    this.sendReset();
                    resolve(response.data.message)
                } catch (error) {
                    if (error.response) {
                        reject(error.response.data.message)
                    } else {
                        reject(error.message)
                    }
                } finally {
                    this.loading = false;
                }
            })
        },
        sendReset() {
            this.phone = '';
            this.type = 'user';
            $("#file").val('');
        },
    },
    template:`
    <div class="blue card" @click="sendModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Audio</div>
            <div class="description">
                Send audio to user or group
            </div>
        </div>
    </div>
    
    <!--  Modal SendAudio  -->
    <div class="ui small modal" id="modalAudioSend">
        <i class="close icon"></i>
        <div class="header">
            Send Audio
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Type</label>
                    <select name="type" v-model="type" aria-label="type">
                        <option value="group">Group Message</option>
                        <option value="user">Private Message</option>
                    </select>
                </div>
                <div class="field">
                    <label>Phone / Group ID</label>
                    <input v-model="phone" type="text" placeholder="6289..."
                           aria-label="phone">
                    <input :value="phone_id" disabled aria-label="whatsapp_id">
                </div>
                <div class="field" style="padding-bottom: 30px">
                    <label></label>
                    <input type="file" class="inputfile" id="file" style="display: none"
                           accept="audio/*"/>
                    <label for="file" class="ui positive medium green left floated button" style="color: white">
                        <i class="ui upload icon"></i>
                        Upload 
                    </label>
                </div>
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.loading}"
                 @click="sendProcess">
                Send
                <i class="send icon"></i>
            </div>
        </div>
    </div>
    `
}