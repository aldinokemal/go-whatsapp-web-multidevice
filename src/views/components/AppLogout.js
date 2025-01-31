export default {
    name: 'AppLogout',
    methods: {
        async handleSubmit() {
            try {
                await this.submitApi()
                showSuccessInfo("Logout success")

                // fetch devices
                this.$emit('reload-devices')
            } catch (err) {
                showErrorInfo(err)
            }
        },

        async submitApi() {
            try {
                await http.get(`app/logout`)
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
            <div class="header">Logout</div>
            <div class="description">
                Remove your login session in application
            </div>
        </div>
    </div>
    `
}