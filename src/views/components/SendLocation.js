import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendLocation',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            latitude: '',
            longitude: '',
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
            this.type = window.TYPEUSER;
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
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                
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