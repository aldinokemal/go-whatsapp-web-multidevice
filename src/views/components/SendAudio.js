import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'Send',
    components: {
        FormRecipient
    },
    data() {
        return {
            phone: '',
            type: window.TYPEUSER,
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        }
    },
    methods: {
        openModal() {
            $('#modalAudioSend').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalAudioSend').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let payload = new FormData();
                payload.append("phone", this.phone_id)
                payload.append("audio", $("#file_audio")[0].files[0])
                const response = await window.http.post(`/send/audio`, payload)
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
            this.phone = '';
            this.type = window.TYPEUSER;
            $("#file_audio").val('');
        },
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
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
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                <div class="field" style="padding-bottom: 30px">
                    <label>Audio</label>
                    <input type="file" style="display: none" accept="audio/*" id="file_audio"/>
                    <label for="file_audio" class="ui positive medium green left floated button" style="color: white">
                        <i class="ui upload icon"></i>
                        Upload 
                    </label>
                </div>
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.loading}"
                 @click="handleSubmit">
                Send
                <i class="send icon"></i>
            </div>
        </div>
    </div>
    `
}