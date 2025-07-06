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
            is_forwarded: false,
            duration: 0
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        },
        isValidForm() {
            // Validate phone number is not empty except for status type
            const isPhoneValid = this.type === window.TYPESTATUS || this.phone.trim().length > 0;
            
            // Validate latitude is between -90 and 90
            const isLatitudeValid = !isNaN(this.latitude) && 
                                  parseFloat(this.latitude) >= -90 && 
                                  parseFloat(this.latitude) <= 90;
            
            // Validate longitude is between -180 and 180
            const isLongitudeValid = !isNaN(this.longitude) && 
                                   parseFloat(this.longitude) >= -180 && 
                                   parseFloat(this.longitude) <= 180;

            return isPhoneValid && isLatitudeValid && isLongitudeValid;
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
        isShowAttributes() {
            return this.type !== window.TYPESTATUS;
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
                    longitude: this.longitude,
                    is_forwarded: this.is_forwarded,
                    ...(this.duration && this.duration > 0 ? {duration: this.duration} : {})
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
            this.is_forwarded = false;
            this.duration = 0;
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
                    <input v-model="latitude" type="text" placeholder="Please enter latitude (-90 to 90)"
                           aria-label="latitude">
                </div>
                <div class="field">
                    <label>Location Longitude</label>
                    <input v-model="longitude" type="text" placeholder="Please enter longitude (-180 to 180)"
                           aria-label="longitude">
                </div>
                <div class="field" v-if="isShowAttributes()">
                    <label>Is Forwarded</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="is forwarded" v-model="is_forwarded">
                        <label>Mark location as forwarded</label>
                    </div>
                </div>
                <div class="field">
                    <label>Disappearing Duration (seconds)</label>
                    <input v-model.number="duration" type="number" min="0" placeholder="0 (no expiry)" aria-label="duration"/>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" :class="{'loading': this.loading}" 
                 @click="handleSubmit" :disabled="!isValidForm">
                Send
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `
}