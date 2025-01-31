export default {
    name: 'AppReconnect',
    methods: {
        async handleSubmit() {
            try {
                await this.submitApi()
                showSuccessInfo("Reconnect success")

                // fetch devices
                this.$emit('reload-devices')
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            try {
                await window.http.get(`/app/reconnect`)
            } catch (error) {
                if (error.response) {
                    throw Error(error.response.data.message)
                }
                throw Error(error.message)
            }
        }
    },
    template: `
    <div class="green card" @click="handleSubmit" style="cursor: pointer">
        <div class="content">
            <a class="ui teal right ribbon label">App</a>
            <div class="header">Reconnect</div>
            <div class="description">
                Please reconnect to the WhatsApp service if your API doesn't work or if your app is down.
            </div>
        </div>
    </div>
    `
}