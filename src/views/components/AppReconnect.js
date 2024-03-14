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
            <div class="header">Reconnect</div>
            <div class="description">
                Reconnect to whatsapp server, please do this if your api doesn't work or your application is down or
                restart
            </div>
        </div>
    </div>
    `
}