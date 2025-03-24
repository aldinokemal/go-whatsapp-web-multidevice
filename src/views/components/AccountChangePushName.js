export default {
    name: 'AccountChangePushName',
    data() {
        return {
            loading: false,
            push_name: ''
        }
    },
    methods: {
        openModal() {
            $('#modalChangePushName').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            return this.push_name.trim() !== '';
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }

            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalChangePushName').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let payload = {
                    push_name: this.push_name
                }

                let response = await window.http.post(`/user/pushname`, payload)
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
            this.push_name = '';
        }
    },
    template: `
    <div class="olive card" @click="openModal()" style="cursor:pointer;">
        <div class="content">
            <a class="ui olive right ribbon label">Account</a>
            <div class="header">Change Push Name</div>
            <div class="description">
                Update your WhatsApp display name
            </div>
        </div>
    </div>
    
    <!--  Modal Change Push Name  -->
    <div class="ui small modal" id="modalChangePushName">
        <i class="close icon"></i>
        <div class="header">
            Change Push Name
        </div>
        <div class="content" style="max-height: 70vh; overflow-y: auto;">
            <div class="ui info message">
                <i class="info circle icon"></i>
                Your push name is the display name shown to others in WhatsApp.
            </div>
            
            <form class="ui form">
                <div class="field">
                    <label>New Push Name</label>
                    <input type="text" v-model="push_name" placeholder="Enter your new display name">
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                 :class="{'loading': this.loading, 'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                Update Push Name
                <i class="save icon"></i>
            </button>
        </div>
    </div>
    `
}
