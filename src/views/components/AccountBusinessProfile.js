import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'AccountBusinessProfile',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            //
            jid: null,
            email: null,
            address: null,
            categories: [],
            profileOptions: {},
            businessHoursTimeZone: null,
            businessHours: [],
            //
            loading: false,
        }
    },

    computed: {
        phone_id() {
            return this.phone + this.type;
        }
    },
    methods: {
        async openModal() {
            this.handleReset();
            $('#modalBusinessProfile').modal('show');
        },
        isValidForm() {
            if (!this.phone.trim()) {
                return false;
            }

            return true;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }

            try {
                await this.submitApi();
                showSuccessInfo("Business profile fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.get(`/user/business-profile?phone=${this.phone_id}`)
                const results = response.data.results;
                this.jid = results.jid;
                this.email = results.email;
                this.address = results.address;
                this.categories = results.categories || [];
                this.profileOptions = results.profile_options || {};
                this.businessHoursTimeZone = results.business_hours_timezone;
                this.businessHours = results.business_hours || [];
            } catch (error) {
                if (error.response) {
                    const message = error.response.data.message;
                    if (message.includes('not be a business account')) {
                        throw new Error('This number is not a WhatsApp Business account or does not have a public business profile.');
                    } else if (message.includes('profile data is corrupted')) {
                        throw new Error('The business profile data appears to be corrupted. Please try again later.');
                    } else {
                        throw new Error(message);
                    }
                } else {
                    throw new Error('Failed to fetch business profile. Please check the phone number and try again.');
                }
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.phone = '';
            this.jid = null;
            this.email = null;
            this.address = null;
            this.categories = [];
            this.profileOptions = {};
            this.businessHoursTimeZone = null;
            this.businessHours = [];
            this.type = window.TYPEUSER;
        },
        formatBusinessHours(hours) {
            if (!hours || hours.length === 0) return 'Not available';
            return hours.map(h => `${h.day_of_week}: ${h.open_time} - ${h.close_time} (${h.mode})`).join(', ');
        }
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer;">
        <div class="content">
        <a class="ui olive right ribbon label">Account</a>
            <div class="header">Business Profile</div>
            <div class="description">
                Get detailed business profile information
            </div>
        </div>
    </div>
    
    
    <!--  Modal Business Profile  -->
    <div class="ui large modal" id="modalBusinessProfile">
        <i class="close icon"></i>
        <div class="header">
            Business Profile Information
        </div>
        <div class="content">
            <form class="ui form">
                <div class="ui info message">
                    <div class="header">
                        <i class="info circle icon"></i>
                        Business Profile Information
                    </div>
                    <p>This feature works only with WhatsApp Business accounts that have a public business profile set up.</p>
                </div>
                
                <FormRecipient v-model:type="type" v-model:phone="phone"/>

                <button type="button" class="ui primary button" :class="{'loading': loading, 'disabled': !this.isValidForm() || this.loading}"
                        @click.prevent="handleSubmit">
                    Get Business Profile
                </button>
            </form>

            <div v-if="jid" class="ui segment" style="margin-top: 20px;">
                <h4 class="ui header">Business Profile Details</h4>
                <div class="ui list">
                    <div class="item">
                        <i class="id badge icon"></i>
                        <div class="content">
                            <div class="header">JID</div>
                            <div class="description">{{ jid }}</div>
                        </div>
                    </div>
                    <div class="item" v-if="email">
                        <i class="mail icon"></i>
                        <div class="content">
                            <div class="header">Email</div>
                            <div class="description">{{ email }}</div>
                        </div>
                    </div>
                    <div class="item" v-if="address">
                        <i class="map marker icon"></i>
                        <div class="content">
                            <div class="header">Address</div>
                            <div class="description">{{ address }}</div>
                        </div>
                    </div>
                    <div class="item" v-if="categories.length > 0">
                        <i class="tags icon"></i>
                        <div class="content">
                            <div class="header">Categories</div>
                            <div class="description">
                                <div class="ui small labels">
                                    <div v-for="category in categories" :key="category.id" class="ui label">
                                        {{ category.name }}
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                    <div class="item" v-if="businessHoursTimeZone">
                        <i class="clock icon"></i>
                        <div class="content">
                            <div class="header">Timezone</div>
                            <div class="description">{{ businessHoursTimeZone }}</div>
                        </div>
                    </div>
                    <div class="item" v-if="businessHours.length > 0">
                        <i class="calendar icon"></i>
                        <div class="content">
                            <div class="header">Business Hours</div>
                            <div class="description">
                                <div class="ui tiny segments">
                                    <div v-for="hours in businessHours" :key="hours.day_of_week" class="ui segment">
                                        <strong>{{ hours.day_of_week.charAt(0).toUpperCase() + hours.day_of_week.slice(1) }}:</strong>
                                        {{ hours.open_time }} - {{ hours.close_time }} ({{ hours.mode }})
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                    <div class="item" v-if="Object.keys(profileOptions).length > 0">
                        <i class="info circle icon"></i>
                        <div class="content">
                            <div class="header">Profile Options</div>
                            <div class="description">
                                <div class="ui list">
                                    <div v-for="(value, key) in profileOptions" :key="key" class="item">
                                        <strong>{{ key }}:</strong> {{ value }}
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
    `
} 