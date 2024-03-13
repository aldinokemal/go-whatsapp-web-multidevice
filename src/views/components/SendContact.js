export default {
    name: 'SendContact',
    data() {
        return {
            type: 'user',
            phone: '',
            card_name: '',
            card_phone: '',
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.type === 'user' ? `${this.phone}@${window.TYPEUSER}` : `${this.phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        openModal() {
            $('#modalSendContact').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            try {
                this.loading = true;
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalSendContact').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            } finally {
                this.loading = false;
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    contact_name: this.card_name,
                    contact_phone: this.card_phone
                }
                let response = await window.http.post(`/send/contact`, payload)
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
            this.card_name = '';
            this.card_phone = '';
            this.type = 'user';
        },
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Contact</div>
            <div class="description">
                Send contact to user or group
            </div>
        </div>
    </div>
    
    <!--  Modal SendContact  -->
    <div class="ui small modal" id="modalSendContact">
        <i class="close icon"></i>
        <div class="header">
            Send Contact
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
                <div class="field">
                    <label>Contact Name</label>
                    <input v-model="card_name" type="text" placeholder="Please enter contact name"
                           aria-label="contact name">
                </div>
                <div class="field">
                    <label>Contact Phone</label>
                    <input v-model="card_phone" type="text" placeholder="Please enter contact phone"
                           aria-label="contact phone">
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