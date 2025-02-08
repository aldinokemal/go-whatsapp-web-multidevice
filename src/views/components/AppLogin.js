export default {
    name: 'AppLogin',
    props: {
        connected: null,
    },
    data() {
        return {
            login_link: '',
            login_duration_sec: 0,
        }
    },
    methods: {
        async openModal() {
            try {
                if (this.connected) throw Error('You are already logged in.');

                await this.submitApi();
                $('#modalLogin').modal({
                    onApprove: function () {
                        return false;
                    }
                }).modal('show');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            try {
                let response = await window.http.get(`app/login`)
                let results = response.data.results;
                this.login_link = results.qr_link;
                this.login_duration_sec = results.qr_duration;
            } catch (error) {
                if (error.response) {
                    throw Error(error.response.data.message)
                }
                throw Error(error.message)
            }
        }
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui teal right ribbon label">App</a>
            <div class="header">Login</div>
            <div class="description">
                Scan your QR code to access all API capabilities.
            </div>
        </div>
    </div>
    
    <!--  Modal Login  -->
    <div class="ui small modal" id="modalLogin">
        <i class="close icon"></i>
        <div class="header">
            Login Whatsapp
        </div>
        <div class="image content">
            <div class="ui medium image">
                <img :src="login_link" alt="qrCodeLogin">
            </div>
            <div class="description">
                <div class="ui header">Please scan to connect</div>
                <p>Open Setting > Linked Devices > Link Device</p>
                <div style="padding-top: 50px;">
                    <i>Refresh QR Code in {{ login_duration_sec }} seconds to avoid link expiration</i>
                </div>
            </div>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" @click="submitApi">
                Refresh QR Code
                <i class="refresh icon"></i>
            </div>
        </div>
    </div>
    `
}