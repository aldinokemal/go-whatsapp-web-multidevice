export default {
    name: 'AccountContact',
    data() {
        return {
            contacts: []
        }
    },
    methods: {
        async openModal() {
            try {
                this.dtClear()
                await this.submitApi();
                $('#modalContactList').modal('show');
                this.dtRebuild()
                showSuccessInfo("Contacts fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        dtClear() {
            $('#account_contacts_table').DataTable().destroy();
        },
        dtRebuild() {
            $('#account_contacts_table').DataTable({
                "pageLength": 10,
                "reloadData": true,
            }).draw();
        },
        async submitApi() {
            try {
                let response = await window.http.get(`/user/my/contacts`)
                this.contacts = response.data.results.data;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            }
        },
        getPhoneNumber(jid) {
            return jid.split('@')[0];
        },
        exportToCSV() {
            if (!this.contacts || this.contacts.length === 0) {
                showErrorInfo("No contacts to export");
                return;
            }
            
            // Create CSV content with headers
            let csvContent = "Phone Number,Name\n";
            
            // Add each contact as a row
            this.contacts.forEach(contact => {
                const phoneNumber = this.getPhoneNumber(contact.jid);
                // Escape commas and quotes in the name field
                const escapedName = contact.name ? contact.name.replace(/"/g, '""') : "";
                csvContent += `${phoneNumber},"${escapedName}"\n`;
            });
            
            // Create a Blob with the CSV data
            const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
            
            // Create a download link and trigger download
            const url = URL.createObjectURL(blob);
            const link = document.createElement('a');
            link.setAttribute('href', url);
            link.setAttribute('download', 'contacts.csv');
            link.style.visibility = 'hidden';
            document.body.appendChild(link);
            link.click();
            document.body.removeChild(link);
            
            showSuccessInfo("Contacts exported to CSV");
        }
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui olive right ribbon label">Contacts</a>
            <div class="header">My Contacts</div>
            <div class="description">
                Display all your contacts
            </div>
        </div>
    </div>
    
    <!--  Modal Contact List  -->
    <div class="ui large modal" id="modalContactList">
        <i class="close icon"></i>
        <div class="header">
            My Contacts
            <button class="ui green right floated button" @click="exportToCSV">
                <i class="download icon"></i> Export to CSV
            </button>
        </div>
        <div class="content">
            <table class="ui celled table" id="account_contacts_table">
                <thead>
                <tr>
                    <th>Phone Number</th>
                    <th>Name</th>
                </tr>
                </thead>
                <tbody v-if="contacts != null">
                <tr v-for="contact in contacts">
                    <td>{{ getPhoneNumber(contact.jid) }}</td>
                    <td>{{ contact.name }}</td>
                </tr>
                </tbody>
            </table>
        </div>
    </div>
    `
}
