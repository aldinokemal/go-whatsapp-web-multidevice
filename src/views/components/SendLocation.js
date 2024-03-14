export default {
    name: 'SendLocation',
    data() {
        return {
            type: 'user',
            phone: '',
            latitude: '',
            longitude: '',
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
            $('#modalSendLocation').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalSendLocation').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    latitude: this.latitude,
                    longitude: this.longitude
                };

                const response = await window.http.post(`/send/location`, payload);
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
            this.latitude = '';
            this.longitude = '';
            this.type = 'user';
        },
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Location</div>
            <div class="description">
                Send location to user or group
            </div>
        </div>
    </div>
    
    <!--  Modal SendLocation  -->
    <div class="ui small modal" id="modalSendLocation">
        <i class="close icon"></i>
        <div class="header">
            Send Location
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
                    <label>Location Latitude</label>
                    <input v-model="latitude" type="text" placeholder="Please enter latitude"
                           aria-label="latitude">
                </div>
                <div class="field">
                    <label>Location Longitude</label>
                    <input v-model="longitude" type="text" placeholder="Please enter longitude"
                           aria-label="longitude">
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