export default {
    name: 'AppLoginWithCode',
    props: {
        connected: null,
    },
    data: () => {
        return {
            phone: '',
            submitting: false,
            pair_code: null,
        };
    },
    methods: {
        async openModal() {
            try {
                $('#modalLoginWithCode').modal({
                    onApprove: function() {
                        return false;
                    },
                }).modal('show');
            } catch (err) {
                showErrorInfo(err);
            }
        },
        async handleSubmit() {
            if (this.submitting) return;
            try {
                this.submitting = true;
                const { response } = await this.submitApi();
                this.pair_code = response.pair_code;
            } catch (err) {
                showErrorInfo(err);
            } finally {
                this.submitting = false;
            }
        },

        async submitApi() {
            try {
                return http.get(`/app/login-with-code`, {
                    params: {
                        phone: this.phone,
                    },
                });
            } catch (error) {
                if (error.response) {
                    throw Error(error.response.data.message);
                }
                throw Error(error.message);
            }

        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <div class="header">Login with Code</div>
            <div class="description">
                Login with pairing code to get access to your devices
            </div>
        </div>
    </div>
    
    <!--  Modal Login  -->
    <div class="ui small modal" id="modalLoginWithCode">
        <i class="close icon"></i>
        <div class="header">
            Getting Pair Code
        </div>
        <div class="content">
            <div class="ui message info">
                <div class="header">How to pair?</div>
                <ol>
                    <li>Open your Whatsapp</li>
                    <li>Link a device</li>
                    <li>Link with pair code</li>
                </ol>
            </div>
            
            <div class="ui form">
                <div class="field">
                    <label>Phone</label>
                    <input type="text" v-model="phone" placeholder="Type your phone number"
                        @keyup.enter="handleSubmit" :disabled="submitting">
                    <small>Enter to submit</small>
                </div>
            </div>
            
            <div class="ui grid" v-if="pair_code">
                <div class="ui two column centered grid">
                    <div class="column center aligned">
                        <div class="header">Pair Code</div>
                        <p style="font-size: 32px">{{ pair_code }}</p>
                        
                    </div>
                </div>
            </div>
        </div>
    </div>
    `,
};